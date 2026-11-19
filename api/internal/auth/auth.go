package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/httpx"
)

const (
	SessionCookieName = "soglia_session"
	SessionTTL        = 8 * time.Hour
	maxLoginFailures  = 5
	loginLockout      = 5 * time.Minute
)

type loginFailure struct {
	count int
	until time.Time
}

// Identity is resolved from the session cookie on every request. Permissions
// are derived from the role in Postgres, never from the browser.
type Identity struct {
	Username string `json:"user"`
	Role     string `json:"role"`
}

type Permission string

const (
	PermModerate Permission = "moderate"
	PermReview   Permission = "review"
	PermFiles    Permission = "files"
	PermTerminal Permission = "terminal"
	PermDB       Permission = "db"
	PermSetup    Permission = "setup"
	PermUsers    Permission = "users"
)

var rolePermissions = map[string][]Permission{
	"admin":     {PermModerate, PermReview, PermFiles, PermTerminal, PermDB, PermSetup, PermUsers},
	"moderator": {PermModerate, PermReview},
	"viewer":    {},
}

func ValidRole(role string) bool {
	_, ok := rolePermissions[role]
	return ok
}

func (id Identity) Can(need Permission) bool {
	for _, granted := range rolePermissions[id.Role] {
		if granted == need {
			return true
		}
	}
	return false
}

func (id Identity) Permissions() []string {
	granted := rolePermissions[id.Role]
	out := make([]string, 0, len(granted))
	for _, p := range granted {
		out = append(out, string(p))
	}
	return out
}

type IdentityContextKey struct{}

func WithIdentity(r *http.Request, id Identity) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), IdentityContextKey{}, id))
}

func CurrentUser(r *http.Request) Identity {
	id, _ := r.Context().Value(IdentityContextKey{}).(Identity)
	return id
}

// Credentials are the password material and role stored for an account.
type Credentials struct {
	Username string
	Salt     []byte
	Hash     []byte
	Role     string
	Enabled  bool
}

// Store is the persistence surface authentication needs. Declaring it here
// keeps auth independent of the Postgres implementation in internal/store.
type Store interface {
	EnsureUser(username, password string) (string, []byte, []byte, string, error)
	UserCredentials(username string) (Credentials, bool, error)
	SessionIdentity(token string) (Identity, bool)
	CreateSession(token, username string) error
	ValidSession(token string) bool
	DestroySession(token string) error
}

// Manager owns the HTTP auth handlers. Accounts and sessions live in
// Postgres; only the short-lived login lockout is in memory.
type Manager struct {
	mu           sync.Mutex
	Store        Store
	failures     map[string]*loginFailure
	secureCookie bool
}

func RandomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return buf
}

func RandomPassword(n int) string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	raw := RandomBytes(n)
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}

func HashPassword(salt, password []byte) []byte {
	sum := sha256.Sum256(append(append([]byte{}, salt...), password...))
	return sum[:]
}

func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func New(s Store, secureCookie bool) (*Manager, error) {
	username := config.EnvOr("SOGLIA_USER", "admin")
	password := config.EnvOr("SOGLIA_PASSWORD", "")

	user, _, _, generated, err := s.EnsureUser(username, password)
	if err != nil {
		return nil, err
	}
	if generated != "" {
		slog.Info("no password configured, generated one", "user", user, "password", generated)
	} else if password != "" {
		slog.Info("using configured account", "user", user)
	} else {
		slog.Info("using account stored in database", "user", user)
	}

	return &Manager{
		Store:        s,
		failures:     make(map[string]*loginFailure),
		secureCookie: secureCookie,
	}, nil
}

func (a *Manager) credentialsMatch(username, password string) (Identity, bool) {
	cred, ok, err := a.Store.UserCredentials(username)
	if err != nil {
		slog.Error("could not read account", "error", err)
		return Identity{}, false
	}
	if !ok || !cred.Enabled {
		return Identity{}, false
	}
	sum := HashPassword(cred.Salt, []byte(password))
	if subtle.ConstantTimeCompare(sum, cred.Hash) != 1 {
		return Identity{}, false
	}
	return Identity{Username: cred.Username, Role: cred.Role}, true
}

func (a *Manager) CreateSession(username string) (string, error) {
	token := hex.EncodeToString(RandomBytes(32))
	if err := a.Store.CreateSession(token, username); err != nil {
		return "", err
	}
	return token, nil
}

func (a *Manager) valid(token string) bool {
	return a.Store.ValidSession(token)
}

func (a *Manager) destroy(token string) {
	if err := a.Store.DestroySession(token); err != nil {
		slog.Error("could not remove session", "error", err)
	}
}

func (a *Manager) locked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.failures[ip]
	return ok && time.Now().Before(f.until) && f.count >= maxLoginFailures
}

func (a *Manager) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.failures[ip]
	if !ok || time.Now().After(f.until) {
		f = &loginFailure{until: time.Now().Add(loginLockout)}
		a.failures[ip] = f
	}
	f.count++
}

func (a *Manager) clearFailures(ip string) {
	a.mu.Lock()
	delete(a.failures, ip)
	a.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SessionKey returns a non-reversible identifier for the current session. It
// lets handlers bind state (such as an in-flight command) to one login without
// keeping the raw cookie value in memory or in logs.
func SessionKey(r *http.Request) string {
	return SHA256Hex(sessionToken(r))
}

func (a *Manager) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(SessionTTL),
		MaxAge:   int(SessionTTL.Seconds()),
	})
}

func (a *Manager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *Manager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	ip := clientIP(r)
	if a.locked(ip) {
		httpx.WriteError(w, http.StatusTooManyRequests, "Too many failed attempts. Try again in a few minutes.")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, ok := a.credentialsMatch(req.Username, req.Password)
	if !ok {
		a.recordFailure(ip)
		httpx.WriteError(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	token, err := a.CreateSession(id.Username)
	if err != nil {
		slog.Error("could not create session", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "Could not start a session.")
		return
	}

	a.clearFailures(ip)
	a.setSessionCookie(w, token)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":        id.Username,
		"role":        id.Role,
		"permissions": id.Permissions(),
	})
}

func (a *Manager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	a.destroy(sessionToken(r))
	a.clearSessionCookie(w)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Manager) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	id, ok := a.Store.SessionIdentity(sessionToken(r))
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":        id.Username,
		"role":        id.Role,
		"permissions": id.Permissions(),
	})
}

// Require blocks API calls without a valid session. RequirePerm additionally
// enforces a role Permission before the handler runs.
func (a *Manager) Require(next http.Handler) http.Handler {
	return a.RequirePerm("", next)
}

func (a *Manager) RequirePerm(needed Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.Store.SessionIdentity(sessionToken(r))
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		if needed != "" && !id.Can(needed) {
			httpx.WriteError(w, http.StatusForbidden, "Your role does not allow this action.")
			return
		}
		next.ServeHTTP(w, WithIdentity(r, id))
	})
}

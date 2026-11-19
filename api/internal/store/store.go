package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/model"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const maxStoredChecks = 5000

// Store owns the PostgreSQL database used by the console: accounts, login
// sessions, and the moderation checks shown in "Recent checks".
type Store struct {
	db *sql.DB
}

type CheckRecord struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Action  string  `json:"action"`
	Tier    string  `json:"tier"`
	Prob    float64 `json:"prob"`
	MS      float64 `json:"ms"`
	Matched string  `json:"matched,omitempty"`
	At      int64   `json:"at"`
}

// ReviewItem is a check in the review band, optionally with its human decision.
// The embedded record keeps the JSON shape identical to a history entry.
type ReviewItem struct {
	CheckRecord
	Decision  string `json:"decision,omitempty"`
	Note      string `json:"note,omitempty"`
	Reviewer  string `json:"reviewer,omitempty"`
	DecidedAt int64  `json:"decided_at,omitempty"`
}

type UserView struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	slog.Info("database ready", "driver", "postgres")
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies embedded SQL files in filename order. Each file runs in its
// own transaction and is recorded in schema_migrations, so applying it twice is
// safe and older databases pick up new files automatically.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(path.Base(file), ".sql")
		var applied bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}

		contents, err := migrationFiles.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		if err := s.applyMigration(version, string(contents)); err != nil {
			return err
		}
		slog.Info("applied database migration", "version", version)
	}
	return nil
}

func (s *Store) applyMigration(version, contents string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer tx.Rollback()

	for _, statement := range strings.Split(contents, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`, version, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}

// EnsureUser returns the account to use. When a password is configured it is
// stored (replacing any previous account). Otherwise an existing account is
// reused, or a random password is created and returned so it can be printed.
func (s *Store) EnsureUser(username, password string) (string, []byte, []byte, string, error) {
	var storedUser, storedSalt, storedHash string
	err := s.db.QueryRow(`SELECT username, salt, password_hash FROM users WHERE id = 1`).Scan(&storedUser, &storedSalt, &storedHash)
	if err != nil && err != sql.ErrNoRows {
		return "", nil, nil, "", fmt.Errorf("read account: %w", err)
	}

	if password != "" {
		// Restarting with the same configured credentials must not invalidate
		// sessions. Only rewrite the account (and drop sessions) when the
		// username or password actually changed.
		storedSaltBytes, saltErr := hex.DecodeString(storedSalt)
		storedHashBytes, hashErr := hex.DecodeString(storedHash)
		if err == nil && saltErr == nil && hashErr == nil && username == storedUser &&
			subtle.ConstantTimeCompare(storedHashBytes, auth.HashPassword(storedSaltBytes, []byte(password))) == 1 {
			return username, storedSaltBytes, storedHashBytes, "", nil
		}

		salt := auth.RandomBytes(16)
		hash := auth.HashPassword(salt, []byte(password))
		_, err := s.db.Exec(`INSERT INTO users (id, username, salt, password_hash, created_at)
			VALUES (1, $1, $2, $3, $4)
			ON CONFLICT (id) DO UPDATE SET username = EXCLUDED.username,
				salt = EXCLUDED.salt, password_hash = EXCLUDED.password_hash`,
			username, hex.EncodeToString(salt), hex.EncodeToString(hash), time.Now().UnixMilli())
		if err != nil {
			return "", nil, nil, "", fmt.Errorf("save account: %w", err)
		}
		// Only this account's sessions are invalidated; other users stay signed in.
		if _, err := s.db.Exec(`DELETE FROM sessions WHERE username = $1 OR username = $2`, username, storedUser); err != nil {
			return "", nil, nil, "", fmt.Errorf("reset sessions: %w", err)
		}
		return username, salt, hash, "", nil
	}

	if err == nil {
		salt, saltErr := hex.DecodeString(storedSalt)
		hash, hashErr := hex.DecodeString(storedHash)
		if saltErr != nil || hashErr != nil {
			return "", nil, nil, "", fmt.Errorf("account has invalid credentials")
		}
		return storedUser, salt, hash, "", nil
	}

	generated := auth.RandomPassword(14)
	salt := auth.RandomBytes(16)
	hash := auth.HashPassword(salt, []byte(generated))
	if _, err := s.db.Exec(`INSERT INTO users (id, username, salt, password_hash, created_at)
		VALUES (1, $1, $2, $3, $4)`, username, hex.EncodeToString(salt), hex.EncodeToString(hash), time.Now().UnixMilli()); err != nil {
		return "", nil, nil, "", fmt.Errorf("create account: %w", err)
	}
	return username, salt, hash, generated, nil
}

func (s *Store) SetUsername(username string) error {
	_, err := s.db.Exec(`UPDATE users SET username = $1 WHERE id = 1`, username)
	return err
}

// Multi-user accounts -------------------------------------------------------

func (s *Store) UserCredentials(username string) (auth.Credentials, bool, error) {
	var cred auth.Credentials
	var saltHex, hashHex string
	err := s.db.QueryRow(`SELECT username, salt, password_hash, role, enabled FROM users WHERE username = $1`, username).
		Scan(&cred.Username, &saltHex, &hashHex, &cred.Role, &cred.Enabled)
	if err == sql.ErrNoRows {
		return auth.Credentials{}, false, nil
	}
	if err != nil {
		return auth.Credentials{}, false, err
	}
	cred.Salt, err = hex.DecodeString(saltHex)
	if err != nil {
		return auth.Credentials{}, false, fmt.Errorf("account has invalid salt")
	}
	cred.Hash, err = hex.DecodeString(hashHex)
	if err != nil {
		return auth.Credentials{}, false, fmt.Errorf("account has invalid password hash")
	}
	return cred, true, nil
}

// SessionIdentity resolves a cookie to its account on every request, so role
// changes and disabled accounts take effect without waiting for a restart.
func (s *Store) SessionIdentity(token string) (auth.Identity, bool) {
	if token == "" {
		return auth.Identity{}, false
	}
	var id auth.Identity
	var enabled bool
	err := s.db.QueryRow(`SELECT u.username, u.role, u.enabled
		FROM sessions s JOIN users u ON u.username = s.username
		WHERE s.token_hash = $1 AND s.expires_at > $2`,
		auth.SHA256Hex(token), time.Now().Unix()).Scan(&id.Username, &id.Role, &enabled)
	if err != nil {
		return auth.Identity{}, false
	}
	if !enabled {
		_ = s.DestroySession(token)
		return auth.Identity{}, false
	}
	return id, true
}

func (s *Store) ListUsers() ([]UserView, error) {
	rows, err := s.db.Query(`SELECT id, username, role, enabled, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]UserView, 0)
	for rows.Next() {
		var user UserView
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.Enabled, &user.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// IsMainAdmin reports whether the account is the bootstrap account (id 1),
// whose credentials are supplied by SOGLIA_USER / SOGLIA_PASSWORD on startup.
func (s *Store) IsMainAdmin(username string) (bool, error) {
	var main bool
	err := s.db.QueryRow(`SELECT id = 1 FROM users WHERE username = $1`, username).Scan(&main)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return main, err
}

func (s *Store) CreateUser(username string, salt, hash []byte, role string) error {
	_, err := s.db.Exec(`INSERT INTO users (username, salt, password_hash, role, enabled, created_at)
		VALUES ($1, $2, $3, $4, TRUE, $5)`,
		username, hex.EncodeToString(salt), hex.EncodeToString(hash), role, time.Now().UnixMilli())
	return err
}

func (s *Store) UpdateUser(username string, newUsername *string, role *string, enabled *bool, salt, hash []byte) (bool, error) {
	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	next := 1
	if newUsername != nil && *newUsername != "" {
		sets = append(sets, fmt.Sprintf("username = $%d", next))
		args = append(args, *newUsername)
		next++
	}
	if role != nil {
		sets = append(sets, fmt.Sprintf("role = $%d", next))
		args = append(args, *role)
		next++
	}
	if enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", next))
		args = append(args, *enabled)
		next++
	}
	if salt != nil {
		sets = append(sets, fmt.Sprintf("salt = $%d", next), fmt.Sprintf("password_hash = $%d", next+1))
		args = append(args, hex.EncodeToString(salt), hex.EncodeToString(hash))
		next += 2
	}
	if len(sets) == 0 {
		return false, fmt.Errorf("nothing to update")
	}
	args = append(args, username)
	result, err := s.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE username = $`+strconv.Itoa(next), args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		if newUsername != nil && *newUsername != "" && *newUsername != username {
			if _, err := s.db.Exec(`UPDATE sessions SET username = $1 WHERE username = $2`, *newUsername, username); err != nil {
				return false, err
			}
		}
		if salt != nil {
			target := username
			if newUsername != nil && *newUsername != "" {
				target = *newUsername
			}
			if err := s.destroyUserSessions(target); err != nil {
				return false, err
			}
		}
	}
	return affected > 0, nil
}

func (s *Store) DeleteUser(username string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM users WHERE username = $1`, username)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE username = $1`, username); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) destroyUserSessions(username string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE username = $1`, username)
	return err
}

func (s *Store) AdminCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND enabled`).Scan(&count)
	return count, err
}

func (s *Store) pruneSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= $1`, time.Now().Unix())
	return err
}

func (s *Store) CreateSession(token, username string) error {
	if err := s.pruneSessions(); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, username, expires_at, created_at) VALUES ($1, $2, $3, $4)`,
		auth.SHA256Hex(token), username, time.Now().Add(auth.SessionTTL).Unix(), time.Now().UnixMilli())
	return err
}

func (s *Store) ValidSession(token string) bool {
	if token == "" {
		return false
	}
	var expires int64
	hash := auth.SHA256Hex(token)
	err := s.db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = $1`, hash).Scan(&expires)
	if err != nil || time.Now().Unix() >= expires {
		if err == nil {
			_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, hash)
		}
		return false
	}
	return true
}

func (s *Store) DestroySession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, auth.SHA256Hex(token))
	return err
}

func (s *Store) RecordCheck(text string, result *model.ModerationResult) error {
	_, err := s.db.Exec(`INSERT INTO checks (text, action, tier, prob, ms, matched, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		text, result.Action, result.Tier, result.Prob, result.MS, result.Matched, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	// Review-band checks are moderator work, not disposable history, so the
	// size cap never prunes them.
	_, err = s.db.Exec(`DELETE FROM checks WHERE id NOT IN (
		SELECT id FROM checks ORDER BY id DESC LIMIT $1
	) AND action <> 'review'`, maxStoredChecks)
	return err
}

func (s *Store) RecentChecks(limit int) ([]CheckRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 12
	}
	rows, err := s.db.Query(`SELECT id, text, action, tier, prob, ms, matched, created_at
		FROM checks ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checks := make([]CheckRecord, 0, limit)
	for rows.Next() {
		var record CheckRecord
		var id int64
		if err := rows.Scan(&id, &record.Text, &record.Action, &record.Tier,
			&record.Prob, &record.MS, &record.Matched, &record.At); err != nil {
			return nil, err
		}
		record.ID = fmt.Sprint(id)
		checks = append(checks, record)
	}
	return checks, rows.Err()
}

// ReviewQueue returns review-band checks with pending items first, then newest
// first. Decided items stay visible so moderators can see what was done.
func (s *Store) ReviewQueue(limit int) ([]ReviewItem, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT c.id, c.text, c.action, c.tier, c.prob, c.ms, c.matched, c.created_at,
		COALESCE(r.decision, ''), COALESCE(r.note, ''), COALESCE(r.reviewer, ''), COALESCE(r.decided_at, 0)
		FROM checks c
		LEFT JOIN reviews r ON r.check_id = c.id
		WHERE c.action = 'review'
		ORDER BY (r.decided_at = 0) DESC, c.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ReviewItem, 0, limit)
	for rows.Next() {
		var item ReviewItem
		var id int64
		if err := rows.Scan(&id, &item.Text, &item.Action, &item.Tier, &item.Prob, &item.MS,
			&item.Matched, &item.At, &item.Decision, &item.Note, &item.Reviewer, &item.DecidedAt); err != nil {
			return nil, err
		}
		item.ID = fmt.Sprint(id)
		items = append(items, item)
	}
	return items, rows.Err()
}

// PendingReviewCount counts review checks with no decision yet.
func (s *Store) PendingReviewCount() (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM checks c
		LEFT JOIN reviews r ON r.check_id = c.id
		WHERE c.action = 'review' AND r.check_id IS NULL`).Scan(&count)
	return count, err
}

// DecideReview records or updates a moderator's decision. It returns false when
// the check does not exist or was not placed in the review band.
func (s *Store) DecideReview(id int64, decision, note, reviewer string) (bool, error) {
	var action string
	err := s.db.QueryRow(`SELECT action FROM checks WHERE id = $1`, id).Scan(&action)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if action != "review" {
		return false, nil
	}
	_, err = s.db.Exec(`INSERT INTO reviews (check_id, decision, note, reviewer, decided_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (check_id) DO UPDATE SET decision = EXCLUDED.decision,
			note = EXCLUDED.note, reviewer = EXCLUDED.reviewer, decided_at = EXCLUDED.decided_at`,
		id, decision, note, reviewer, time.Now().UnixMilli())
	return err == nil, err
}

// ClearReview removes a decision so the check returns to the pending queue.
// The check itself is kept — only the moderator's decision is discarded.
func (s *Store) ClearReview(id int64) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM reviews WHERE check_id = $1`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *Store) ClearChecks() error {
	// Clearing history must not throw away the moderator queue or the decisions
	// already made on it. Those checks stay until they are explicitly removed.
	_, err := s.db.Exec(`DELETE FROM checks WHERE action <> 'review'`)
	return err
}

func (s *Store) StatsSnapshot() (map[string]any, error) {
	var total, allow, review, block, tier1, tier2, fastPath int64
	var avgMS float64
	err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN action = 'allow' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'review' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN action = 'block' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN tier = 'Tier 1' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN tier = 'Tier 2' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN tier = 'FastPath' THEN 1 ELSE 0 END), 0),
		COALESCE(AVG(ms), 0)
		FROM checks`).Scan(&total, &allow, &review, &block, &tier1, &tier2, &fastPath, &avgMS)
	if err != nil {
		return nil, err
	}
	pending, err := s.PendingReviewCount()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"total":           total,
		"allow":           allow,
		"review":          review,
		"pending_reviews": pending,
		"block":           block,
		"tier1":           tier1,
		"tier2":           tier2,
		"fastpath":        fastPath,
		"avg_ms":          math.Round(avgMS*100) / 100,
	}, nil
}

// Read-only database browser -------------------------------------------------
// These helpers back the console's Database tab. All reads are authenticated;
// credential columns are masked before they ever leave the API.

const maxDbRows = 200

var maskedDbColumns = map[string]bool{
	"password":      true,
	"password_hash": true,
	"salt":          true,
	"token_hash":    true,
}

type DBTable struct {
	Name    string   `json:"name"`
	Rows    int64    `json:"rows"`
	Columns []string `json:"columns"`
}

type DBResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated"`
}

func dbCell(column string, value any) any {
	if maskedDbColumns[strings.ToLower(column)] {
		return "********"
	}
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}
		return fmt.Sprintf("<binary %d bytes>", len(v))
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return v
	}
}

func scanDBRows(rows *sql.Rows) (DBResult, error) {
	columns, err := rows.Columns()
	if err != nil {
		return DBResult{}, err
	}
	result := DBResult{Columns: columns, Rows: make([]map[string]any, 0)}
	for rows.Next() {
		if len(result.Rows) >= maxDbRows {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return DBResult{}, err
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			row[column] = dbCell(column, values[i])
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

func (s *Store) DBTables() ([]DBTable, error) {
	rows, err := s.db.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]DBTable, 0)
	for rows.Next() {
		var table DBTable
		if err := rows.Scan(&table.Name); err != nil {
			return nil, err
		}
		table.Columns = []string{}
		columns, err := s.db.Query(`SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position`, table.Name)
		if err != nil {
			return nil, err
		}
		for columns.Next() {
			var column string
			if err := columns.Scan(&column); err != nil {
				columns.Close()
				return nil, err
			}
			table.Columns = append(table.Columns, column)
		}
		err = columns.Err()
		columns.Close()
		if err != nil {
			return nil, err
		}
		if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", pgx.Identifier{table.Name}.Sanitize())).Scan(&table.Rows); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func (s *Store) DBTableRows(name string) (DBResult, error) {
	if name == "" {
		return DBResult{}, fmt.Errorf("table name is required")
	}
	rows, err := s.db.Query(fmt.Sprintf("SELECT * FROM %s ORDER BY 1 DESC LIMIT %d",
		pgx.Identifier{name}.Sanitize(), maxDbRows))
	if err != nil {
		return DBResult{}, err
	}
	defer rows.Close()
	return scanDBRows(rows)
}

// DBExec runs an admin-supplied DELETE in its own transaction. The handler
// classifies the statement first; this path is only used for deletes.
func (s *Store) DBExec(query string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

// DBQuery runs an operator-supplied statement in a read-only transaction so a
// write cannot slip through even if the prefix check in the handler is bypassed.
func (s *Store) DBQuery(query string) (DBResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DBResult{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return DBResult{}, err
	}
	defer rows.Close()
	return scanDBRows(rows)
}

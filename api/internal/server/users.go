package server

import (
	"log/slog"
	"net/http"
	"strings"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/httpx"
)

const minPasswordLength = 8

func cleanUsername(value string) string {
	return strings.TrimSpace(value)
}

func validPassword(value string) bool {
	return len(value) >= minPasswordLength && len(value) <= 200
}

// handleUsers exposes admin-only account management. The route is wrapped with
// permUsers, so every method here is already restricted to admins.
func (a *api) handleUsers(w http.ResponseWriter, r *http.Request) {
	actor := auth.CurrentUser(r).Username
	switch r.Method {
	case http.MethodGet:
		users, err := a.store.ListUsers()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read accounts.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users})

	case http.MethodPost:
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := httpx.DecodeBody(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		username := cleanUsername(req.Username)
		if len(username) < 1 || len(username) > 64 {
			httpx.WriteError(w, http.StatusBadRequest, "username must be 1 to 64 characters")
			return
		}
		if !validPassword(req.Password) {
			httpx.WriteError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		if !auth.ValidRole(req.Role) {
			httpx.WriteError(w, http.StatusBadRequest, "role must be admin, moderator or viewer")
			return
		}
		salt := auth.RandomBytes(16)
		if err := a.store.CreateUser(username, salt, auth.HashPassword(salt, []byte(req.Password)), req.Role); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
				httpx.WriteError(w, http.StatusConflict, "That username is already in use.")
				return
			}
			httpx.WriteError(w, http.StatusInternalServerError, "Could not create account.")
			return
		}
		slog.Info("user created", "user", username, "role", req.Role, "by", actor)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "username": username})

	case http.MethodPut:
		var req struct {
			Username string  `json:"username"`
			Role     *string `json:"role"`
			Enabled  *bool   `json:"enabled"`
			Password *string `json:"password"`
		}
		if err := httpx.DecodeBody(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		username := cleanUsername(req.Username)
		if username == "" {
			httpx.WriteError(w, http.StatusBadRequest, "username is required")
			return
		}
		if req.Role != nil && !auth.ValidRole(*req.Role) {
			httpx.WriteError(w, http.StatusBadRequest, "role must be admin, moderator or viewer")
			return
		}
		if req.Password != nil && !validPassword(*req.Password) {
			httpx.WriteError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		current, ok, err := a.store.UserCredentials(username)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read account.")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "Account not found.")
			return
		}
		// The bootstrap admin's password is owned by SOGLIA_PASSWORD. Letting an
		// admin reset it here would be silently undone on the next restart.
		main, err := a.store.IsMainAdmin(username)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read account.")
			return
		}
		if main && req.Password != nil {
			httpx.WriteError(w, http.StatusBadRequest, "The main admin password cannot be reset here. Change SOGLIA_PASSWORD and restart instead.")
			return
		}
		newRole, newEnabled := current.Role, current.Enabled
		if req.Role != nil {
			newRole = *req.Role
		}
		if req.Enabled != nil {
			newEnabled = *req.Enabled
		}
		if current.Role == "admin" && current.Enabled && (newRole != "admin" || !newEnabled) {
			count, err := a.store.AdminCount()
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "Could not check admin accounts.")
				return
			}
			if count <= 1 {
				httpx.WriteError(w, http.StatusBadRequest, "At least one enabled admin is required.")
				return
			}
		}
		var salt, hash []byte
		if req.Password != nil {
			salt = auth.RandomBytes(16)
			hash = auth.HashPassword(salt, []byte(*req.Password))
		}
		updated, err := a.store.UpdateUser(username, nil, req.Role, req.Enabled, salt, hash)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not update account.")
			return
		}
		if !updated {
			httpx.WriteError(w, http.StatusNotFound, "Account not found.")
			return
		}
		slog.Info("user updated", "user", username, "role", newRole, "enabled", newEnabled, "by", actor)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		username := cleanUsername(r.URL.Query().Get("username"))
		if username == "" {
			httpx.WriteError(w, http.StatusBadRequest, "username is required")
			return
		}
		if username == actor {
			httpx.WriteError(w, http.StatusBadRequest, "You cannot remove your own account.")
			return
		}
		current, ok, err := a.store.UserCredentials(username)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read account.")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "Account not found.")
			return
		}
		if current.Role == "admin" && current.Enabled {
			count, err := a.store.AdminCount()
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "Could not check admin accounts.")
				return
			}
			if count <= 1 {
				httpx.WriteError(w, http.StatusBadRequest, "At least one enabled admin is required.")
				return
			}
		}
		if _, err := a.store.DeleteUser(username); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not remove account.")
			return
		}
		slog.Info("user removed", "user", username, "by", actor)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})

	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET, POST, PUT or DELETE required")
	}
}

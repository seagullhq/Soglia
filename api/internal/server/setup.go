package server

import (
	"log/slog"
	"net/http"
	"strings"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/httpx"
)

func (a *api) setupView(r *http.Request) map[string]any {
	settings := config.LoadSettings()
	serviceChanged := settings.DatabaseURL != a.opts.DBURL ||
		settings.ModelDir != a.opts.ModelDir ||
		settings.Python != a.opts.Python ||
		settings.Worker != a.opts.Worker ||
		settings.SecureCookie != a.opts.SecureCookie

	return map[string]any{
		"account": map[string]any{
			"username": auth.CurrentUser(r).Username,
			"role":     auth.CurrentUser(r).Role,
		},
		"settings": map[string]any{
			"database_url":  config.MaskDSN(settings.DatabaseURL),
			"model_dir":     settings.ModelDir,
			"python":        settings.Python,
			"worker":        settings.Worker,
			"secure_cookie": settings.SecureCookie,
		},
		"status": map[string]any{
			"settings_file":    config.SettingsFilePath(),
			"database":         "connected",
			"model_dir_exists": config.PathExists(a.opts.ModelDir),
			"worker_exists":    config.PathExists(a.opts.Worker),
			"restart_required": serviceChanged,
		},
	}
}

func (a *api) handleSetup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.WriteJSON(w, http.StatusOK, a.setupView(r))
	case http.MethodPut:
		var req config.SetupUpdate
		if err := httpx.DecodeBody(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}

		settings := config.LoadSettings()
		changed := false
		if req.DatabaseURL != "" && req.DatabaseURL != config.MaskedSecret && req.DatabaseURL != config.MaskDSN(settings.DatabaseURL) {
			settings.DatabaseURL = req.DatabaseURL
			changed = true
		}
		if req.ModelDir != "" && req.ModelDir != settings.ModelDir {
			settings.ModelDir = req.ModelDir
			changed = true
		}
		if req.Python != "" && req.Python != settings.Python {
			settings.Python = req.Python
			changed = true
		}
		if req.Worker != "" && req.Worker != settings.Worker {
			settings.Worker = req.Worker
			changed = true
		}
		if req.SecureCookie != nil && *req.SecureCookie != settings.SecureCookie {
			settings.SecureCookie = *req.SecureCookie
			changed = true
		}
		if changed {
			if err := config.SaveSettings(settings); err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}

		accountChanged := false
		actor := auth.CurrentUser(r).Username
		newUsername := cleanUsername(req.Username)
		if newUsername == "" {
			newUsername = actor
		}
		if req.Password != "" || newUsername != actor {
			if len(newUsername) < 1 || len(newUsername) > 64 {
				httpx.WriteError(w, http.StatusBadRequest, "username must be 1 to 64 characters")
				return
			}
			if req.Password != "" && !validPassword(req.Password) {
				httpx.WriteError(w, http.StatusBadRequest, "password must be at least 8 characters")
				return
			}
			var salt, hash []byte
			if req.Password != "" {
				salt = auth.RandomBytes(16)
				hash = auth.HashPassword(salt, []byte(req.Password))
			}
			if _, err := a.store.UpdateUser(actor, &newUsername, nil, nil, salt, hash); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
					httpx.WriteError(w, http.StatusConflict, "That username is already in use.")
					return
				}
				httpx.WriteError(w, http.StatusInternalServerError, "Could not update account.")
				return
			}
			slog.Info("account updated", "user", newUsername, "by", actor)
			accountChanged = true
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"settings_changed": changed,
			"account_changed":  accountChanged,
		})
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET or PUT required")
	}
}

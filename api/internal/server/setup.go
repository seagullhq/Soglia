package server

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/httpx"
)

// terminalEnabled reports the desired terminal state; older settings files
// without the field keep the terminal on.
func terminalEnabled(settings config.AppSettings) bool {
	return settings.TerminalEnabled == nil || *settings.TerminalEnabled
}

func floatValue(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// validateServiceSettings rejects values that would stop the API from
// starting or the worker from classifying text after a restart.
func validateServiceSettings(settings config.AppSettings) error {
	addr := strings.TrimSpace(settings.Addr)
	if addr == "" {
		return fmt.Errorf("listen address cannot be empty")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("listen address must be host:port (e.g. :8080)")
	}
	if (settings.TLSCert == "") != (settings.TLSKey == "") {
		return fmt.Errorf("TLS certificate and key must be set together")
	}
	if settings.Temperature != nil {
		if value := *settings.Temperature; !finite(value) || value <= 0 || value > 100 {
			return fmt.Errorf("temperature must be greater than 0 and at most 100")
		}
	}
	if settings.BlockThreshold != nil {
		if value := *settings.BlockThreshold; !finite(value) || value < 0 || value > 1 {
			return fmt.Errorf("block threshold must be between 0 and 1")
		}
	}
	if settings.ReviewThreshold != nil {
		if value := *settings.ReviewThreshold; !finite(value) || value < 0 || value > 1 {
			return fmt.Errorf("review threshold must be between 0 and 1")
		}
	}
	return nil
}

// currentSettings loads the saved settings, filling fields from older files
// with the values the running process is actually using. That keeps the form
// accurate and avoids wiping new fields when an old settings file is saved.
func (a *api) currentSettings() config.AppSettings {
	settings := config.LoadSettings()
	if settings.DatabaseURL == "" {
		settings.DatabaseURL = a.opts.DBURL
	}
	if settings.ModelDir == "" {
		settings.ModelDir = a.opts.ModelDir
	}
	if settings.Python == "" {
		settings.Python = a.opts.Python
	}
	if settings.Worker == "" {
		settings.Worker = a.opts.Worker
	}
	if strings.TrimSpace(settings.Addr) == "" {
		settings.Addr = a.opts.Addr
	}
	if settings.TLSCert == "" {
		settings.TLSCert = a.opts.TLSCert
	}
	if settings.TLSKey == "" {
		settings.TLSKey = a.opts.TLSKey
	}
	if settings.TerminalEnabled == nil {
		enabled := a.opts.TerminalEnabled
		settings.TerminalEnabled = &enabled
	}
	if settings.Temperature == nil {
		value := a.model.Temperature
		settings.Temperature = &value
	}
	if settings.BlockThreshold == nil {
		value := a.model.BlockThreshold
		settings.BlockThreshold = &value
	}
	if settings.ReviewThreshold == nil {
		value := a.model.ReviewThreshold
		settings.ReviewThreshold = &value
	}
	return settings
}

func (a *api) setupView(r *http.Request) map[string]any {
	settings := a.currentSettings()
	serviceChanged := settings.DatabaseURL != a.opts.DBURL ||
		settings.ModelDir != a.opts.ModelDir ||
		settings.Python != a.opts.Python ||
		settings.Worker != a.opts.Worker ||
		settings.SecureCookie != a.opts.SecureCookie ||
		settings.Addr != a.opts.Addr ||
		settings.TLSCert != a.opts.TLSCert ||
		settings.TLSKey != a.opts.TLSKey ||
		terminalEnabled(settings) != a.opts.TerminalEnabled ||
		floatValue(settings.Temperature, 0) != a.model.Temperature ||
		floatValue(settings.BlockThreshold, 0) != a.model.BlockThreshold ||
		floatValue(settings.ReviewThreshold, 0) != a.model.ReviewThreshold

	return map[string]any{
		"account": map[string]any{
			"username": auth.CurrentUser(r).Username,
			"role":     auth.CurrentUser(r).Role,
		},
		"settings": map[string]any{
			"database_url":     config.MaskDSN(settings.DatabaseURL),
			"model_dir":        settings.ModelDir,
			"python":           settings.Python,
			"worker":           settings.Worker,
			"secure_cookie":    settings.SecureCookie,
			"addr":             settings.Addr,
			"tls_cert":         settings.TLSCert,
			"tls_key":          settings.TLSKey,
			"terminal_enabled": terminalEnabled(settings),
			"temperature":      floatValue(settings.Temperature, 0),
			"block_threshold":  floatValue(settings.BlockThreshold, 0),
			"review_threshold": floatValue(settings.ReviewThreshold, 0),
		},
		"status": map[string]any{
			"settings_file":    config.SettingsFilePath(),
			"database":         "connected",
			"model_dir_exists": config.PathExists(a.opts.ModelDir),
			"worker_exists":    config.PathExists(a.opts.Worker),
			"https":            a.opts.TLSEnabled(),
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

		settings := a.currentSettings()
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
		if req.Addr != nil {
			addr := strings.TrimSpace(*req.Addr)
			if addr != settings.Addr {
				settings.Addr = addr
				changed = true
			}
		}
		if req.TLSCert != nil {
			cert := strings.TrimSpace(*req.TLSCert)
			if cert != settings.TLSCert {
				settings.TLSCert = cert
				changed = true
			}
		}
		if req.TLSKey != nil {
			key := strings.TrimSpace(*req.TLSKey)
			if key != settings.TLSKey {
				settings.TLSKey = key
				changed = true
			}
		}
		if req.TerminalEnabled != nil && *req.TerminalEnabled != terminalEnabled(settings) {
			settings.TerminalEnabled = req.TerminalEnabled
			changed = true
		}
		if req.Temperature != nil && *req.Temperature != floatValue(settings.Temperature, 0) {
			settings.Temperature = req.Temperature
			changed = true
		}
		if req.BlockThreshold != nil && *req.BlockThreshold != floatValue(settings.BlockThreshold, 0) {
			settings.BlockThreshold = req.BlockThreshold
			changed = true
		}
		if req.ReviewThreshold != nil && *req.ReviewThreshold != floatValue(settings.ReviewThreshold, 0) {
			settings.ReviewThreshold = req.ReviewThreshold
			changed = true
		}

		if err := validateServiceSettings(settings); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
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

// handleSetupReset clears every stored service setting. Startup then falls
// back to flags, environment variables, auto-detection and the model's
// calibration.json. Console accounts are untouched.
func (a *api) handleSetupReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if err := config.SaveSettings(config.AppSettings{}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("service settings restored to defaults", "by", auth.CurrentUser(r).Username)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"settings_changed": true,
	})
}

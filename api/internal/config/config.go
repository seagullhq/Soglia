// Package config loads startup options and the small file-backed settings the
// Setup page can edit.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"profanity-filter-api/internal/model"
)

// Options is the resolved server configuration. Flags and environment
// variables take precedence over the values stored in settings.json.
type Options struct {
	Addr         string
	Python       string
	Worker       string
	ModelDir     string
	DBURL        string
	SecureCookie bool
	TLSCert      string
	TLSKey       string

	// TerminalEnabled controls whether the read-only diagnostic terminal is
	// registered. Settings can disable it; the default is enabled.
	TerminalEnabled bool

	// CalibrationOverride holds any thresholds set from the Setup page. Nil
	// fields fall back to the model's calibration.json.
	model.CalibrationOverride
}

func (o Options) TLSEnabled() bool {
	return o.TLSCert != "" && o.TLSKey != ""
}

// Fatalf prints an error to stderr and exits. Used for startup misconfiguration.
func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// EnvOr returns a trimmed environment value or the given fallback.
func EnvOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// OrDefault returns value unless it is empty.
func OrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// EnvBool parses a boolean environment variable, falling back when unset or invalid.
func EnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

// ParseOptions resolves flags, environment variables and the settings file,
// then auto-detects worker.py and the v6 model relative to the binary/CWD.
func ParseOptions() Options {
	var opts Options
	saved := LoadSettings()
	terminalDefault := true
	if saved.TerminalEnabled != nil {
		terminalDefault = *saved.TerminalEnabled
	}
	opts.CalibrationOverride = saved.CalibrationOverride
	flag.StringVar(&opts.Addr, "addr", OrDefault(saved.Addr, ":8080"), "listen address (e.g. :8080 or 127.0.0.1:8080)")
	flag.StringVar(&opts.Python, "python", OrDefault(saved.Python, "python"), "Python executable that has onnxruntime, tokenizers and numpy")
	flag.StringVar(&opts.Worker, "worker", saved.Worker, "path to worker.py (auto-detected when empty)")
	flag.StringVar(&opts.ModelDir, "model", saved.ModelDir, "model directory containing v6 files (auto-detected when empty)")
	flag.StringVar(&opts.DBURL, "db-url", EnvOr("SOGLIA_DATABASE_URL", EnvOr("DATABASE_URL", OrDefault(saved.DatabaseURL, "postgres://soglia:soglia@localhost:5432/soglia?sslmode=verify-full&sslrootcert=certs/ca.crt"))),
		"PostgreSQL connection URL (or set DATABASE_URL / SOGLIA_DATABASE_URL)")
	flag.BoolVar(&opts.SecureCookie, "secure-cookie", saved.SecureCookie, "add Secure to session cookies (automatic when HTTPS is enabled)")
	flag.BoolVar(&opts.TerminalEnabled, "terminal", EnvBool("SOGLIA_TERMINAL_ENABLED", terminalDefault), "enable the read-only diagnostic terminal (on by default)")
	flag.StringVar(&opts.TLSCert, "tls-cert", EnvOr("SOGLIA_TLS_CERT", saved.TLSCert), "TLS certificate file; together with -tls-key enables HTTPS")
	flag.StringVar(&opts.TLSKey, "tls-key", EnvOr("SOGLIA_TLS_KEY", saved.TLSKey), "TLS private key file; together with -tls-cert enables HTTPS")
	flag.Parse()

	cwd, _ := os.Getwd()
	exeDir := ExecutableDir()

	if opts.Worker == "" {
		opts.Worker = FirstExistingFile(
			filepath.Join(exeDir, "worker.py"),
			filepath.Join(cwd, "worker.py"),
			filepath.Join(cwd, "api", "worker.py"),
		)
		if opts.Worker == "" {
			Fatalf("worker.py not found; pass -worker /path/to/worker.py")
		}
	}
	if opts.ModelDir == "" {
		opts.ModelDir = firstModelDir(
			filepath.Join(exeDir, "..", "v6"),
			filepath.Join(cwd, "..", "v6"),
			filepath.Join(cwd, "v6"),
			filepath.Join(cwd, "api", "..", "v6"),
		)
		if opts.ModelDir == "" {
			Fatalf("v6 model directory not found; pass -model /path/to/v6")
		}
	}

	opts.Worker = absPath(opts.Worker)
	opts.ModelDir = absPath(opts.ModelDir)
	for _, name := range []string{"model_int8.onnx", "tokenizer.json", "calibration.json", "clean_blocklist.txt"} {
		if !isFile(filepath.Join(opts.ModelDir, name)) {
			Fatalf("model file missing: %s", filepath.Join(opts.ModelDir, name))
		}
	}
	return opts
}

// ExecutableDir is the directory containing the running binary.
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// FirstExistingFile returns the first candidate that is an existing file.
func FirstExistingFile(candidates ...string) string {
	for _, p := range candidates {
		if p != "" && isFile(p) {
			return p
		}
	}
	return ""
}

func firstModelDir(candidates ...string) string {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if isFile(filepath.Join(p, "model_int8.onnx")) {
			return p
		}
	}
	return ""
}

// FirstExistingDir returns the first candidate that is an existing directory.
func FirstExistingDir(candidates ...string) string {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// AppSettings is the small file-backed configuration the Setup page can edit.
// Explicit flags/env vars still take precedence when the API starts.
type AppSettings struct {
	DatabaseURL     string `json:"database_url"`
	ModelDir        string `json:"model_dir"`
	Python          string `json:"python"`
	Worker          string `json:"worker"`
	SecureCookie    bool   `json:"secure_cookie"`
	Addr            string `json:"addr"`
	TLSCert         string `json:"tls_cert"`
	TLSKey          string `json:"tls_key"`
	TerminalEnabled *bool  `json:"terminal_enabled,omitempty"`
	model.CalibrationOverride
}

const MaskedSecret = "********"

func SettingsFilePath() string {
	return EnvOr("SOGLIA_SETTINGS", "settings.json")
}

func LoadSettings() AppSettings {
	var settings AppSettings
	contents, err := os.ReadFile(SettingsFilePath())
	if err != nil {
		return settings
	}
	if err := json.Unmarshal(contents, &settings); err != nil {
		slog.Warn("could not parse settings file; using defaults", "error", err)
		return AppSettings{}
	}
	return settings
}

func SaveSettings(settings AppSettings) error {
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(SettingsFilePath(), contents, 0o600); err != nil {
		return fmt.Errorf("write settings file: %w", err)
	}
	return nil
}

func EnsureSettingsFile(opts Options) {
	if _, err := os.Stat(SettingsFilePath()); err == nil {
		return
	}
	terminal := opts.TerminalEnabled
	settings := AppSettings{
		DatabaseURL:         opts.DBURL,
		ModelDir:            opts.ModelDir,
		Python:              opts.Python,
		Worker:              opts.Worker,
		SecureCookie:        opts.SecureCookie,
		Addr:                opts.Addr,
		TLSCert:             opts.TLSCert,
		TLSKey:              opts.TLSKey,
		TerminalEnabled:     &terminal,
		CalibrationOverride: opts.CalibrationOverride,
	}
	if err := SaveSettings(settings); err != nil {
		slog.Warn("could not create settings file", "error", err)
	}
}

func MaskDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return dsn
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return dsn
	}
	// Replace in the original string so the mask stays readable and is not
	// percent-encoded as %2A by url.String().
	return strings.Replace(dsn, ":"+password+"@", ":"+MaskedSecret+"@", 1)
}

// SetupUpdate is the request body accepted by PUT /setup.
type SetupUpdate struct {
	Username        string  `json:"username,omitempty"`
	Password        string  `json:"password,omitempty"`
	DatabaseURL     string  `json:"database_url,omitempty"`
	ModelDir        string  `json:"model_dir,omitempty"`
	Python          string  `json:"python,omitempty"`
	Worker          string  `json:"worker,omitempty"`
	SecureCookie    *bool   `json:"secure_cookie,omitempty"`
	Addr            *string `json:"addr,omitempty"`
	TLSCert         *string `json:"tls_cert,omitempty"`
	TLSKey          *string `json:"tls_key,omitempty"`
	TerminalEnabled *bool   `json:"terminal_enabled,omitempty"`
	model.CalibrationOverride
}

func PathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

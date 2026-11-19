package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"profanity-filter-api/internal/httpx"
)

// maxFilePreviewBytes caps how much of a file is read into memory. Larger files
// are still listed with their real size, but only a truncated preview is shown.
const (
	maxFilePreviewBytes = 256 << 10
	// filesRoot is the only directory the Files tab may browse. Symlinks are
	// resolved before this check, so links out of /app are rejected too.
	filesRoot = "/app"
)

var (
	// Mask the password inside a PostgreSQL connection string.
	dsnPasswordRE = regexp.MustCompile(`(postgres(?:ql)?://[^:/\s]+:)[^@\s]+(@)`)
	// Mask common JSON credential fields.
	jsonSecretRE = regexp.MustCompile(`("(?:password|secret|token|api[_-]?key)"\s*:\s*)"[^"]*"`)
	// Mask sensitive KEY=value assignments in .env-style files.
	envSecretRE = regexp.MustCompile(`(?mi)^([A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|API_KEY)[A-Z0-9_]*=).+$`)

	secretExtensions = []string{".key", ".pem", ".p12", ".pfx", ".ppk", ".kdbx", ".keystore"}
	secretNames      = map[string]string{
		".netrc":           "credentials file",
		".npmrc":           "credentials file",
		".htpasswd":        "credentials file",
		"credentials":      "credentials file",
		"credentials.json": "credentials file",
		"shadow":           "system credentials",
		"passwd":           "system credentials",
	}
)

type fileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	Locked  bool   `json:"locked"`
	Reason  string `json:"reason,omitempty"`
}

type filePreview struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated"`
	Locked    bool   `json:"locked"`
	Reason    string `json:"reason,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
}

type filesResponse struct {
	Path    string       `json:"path"`
	Parent  string       `json:"parent"`
	IsDir   bool         `json:"is_dir"`
	Entries []fileEntry  `json:"entries,omitempty"`
	File    *filePreview `json:"file,omitempty"`
}

// handleFiles serves a directory listing or a text preview, confined to
// /app. Secret material inside that tree is never returned.
func (a *api) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	abs, err := resolveFilePath(r.URL.Query().Get("path"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "path not found or not readable")
		return
	}

	parent := filepath.Dir(abs)
	if parent == abs || !withinFilesRoot(parent) {
		parent = ""
	}
	result := filesResponse{Path: abs, Parent: parent}

	if info.IsDir() {
		entries, err := os.ReadDir(abs)
		if err != nil {
			httpx.WriteError(w, http.StatusForbidden, "directory cannot be read")
			return
		}
		result.IsDir = true
		result.Entries = make([]fileEntry, 0, len(entries))
		for _, entry := range entries {
			item := fileEntry{Name: entry.Name(), Path: filepath.Join(abs, entry.Name())}
			item.IsDir = entry.IsDir()
			if meta, err := entry.Info(); err == nil {
				item.Size = meta.Size()
				item.ModTime = meta.ModTime().Format("2006-01-02 15:04")
			}
			if !item.IsDir {
				item.Locked, item.Reason = fileLockReason(item.Name)
			}
			result.Entries = append(result.Entries, item)
		}
		sort.Slice(result.Entries, func(i, j int) bool {
			a, b := result.Entries[i], result.Entries[j]
			if a.IsDir != b.IsDir {
				return a.IsDir
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		})
	} else {
		result.File = previewFile(abs, info)
	}

	httpx.WriteJSON(w, http.StatusOK, result)
}

// filesRootPath resolves the root once, so a symlinked /app still works.
func filesRootPath() string {
	if real, err := filepath.EvalSymlinks(filesRoot); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(filesRoot)
}

func withinFilesRoot(path string) bool {
	root := filesRootPath()
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func resolveFilePath(raw string) (string, error) {
	abs := filesRoot
	if strings.TrimSpace(raw) != "" {
		var err error
		abs, err = filepath.Abs(raw)
		if err != nil {
			return "", err
		}
	}
	abs = filepath.Clean(abs)
	// Follow symlinks so confinement is checked against the real target.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(real)
	}
	if !withinFilesRoot(abs) {
		return "", fmt.Errorf("path must be inside %s", filesRoot)
	}
	return abs, nil
}

// fileLockReason returns a reason when a file's contents must not be exposed.
func fileLockReason(name string) (bool, string) {
	lower := strings.ToLower(name)
	if lower == ".env" || (strings.HasPrefix(lower, ".env.") && lower != ".env.example") {
		return true, "environment secrets are hidden"
	}
	for _, ext := range secretExtensions {
		if strings.HasSuffix(lower, ext) {
			return true, "private key / credential store"
		}
	}
	if strings.HasPrefix(lower, "id_rsa") || strings.HasPrefix(lower, "id_ed25519") || strings.HasPrefix(lower, "id_ecdsa") {
		return true, "SSH private key"
	}
	if reason, ok := secretNames[lower]; ok {
		return true, reason
	}
	if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dll") || strings.HasSuffix(lower, ".so") ||
		strings.HasSuffix(lower, ".onnx") || strings.HasSuffix(lower, ".safetensors") || strings.HasSuffix(lower, ".data") ||
		strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") {
		return true, "binary file"
	}
	return false, ""
}

func previewFile(abs string, info os.FileInfo) *filePreview {
	name := filepath.Base(abs)
	result := &filePreview{Name: name, Path: abs, Size: info.Size()}
	if locked, reason := fileLockReason(name); locked {
		result.Locked = true
		result.Reason = reason
		return result
	}

	file, err := os.Open(abs)
	if err != nil {
		result.Locked = true
		result.Reason = "file cannot be read"
		return result
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxFilePreviewBytes+1))
	if err != nil {
		result.Locked = true
		result.Reason = "file cannot be read"
		return result
	}
	if len(data) > maxFilePreviewBytes {
		result.Truncated = true
		data = data[:maxFilePreviewBytes]
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		result.Binary = true
		result.Reason = "binary file — no text preview"
		return result
	}

	content := string(data)
	lower := strings.ToLower(name)
	if lower == "settings.json" || lower == ".env.example" || strings.HasPrefix(lower, "settings.") {
		content = maskSecrets(content)
	}
	result.Content = content
	return result
}

func maskSecrets(content string) string {
	content = dsnPasswordRE.ReplaceAllString(content, "${1}********${2}")
	content = jsonSecretRE.ReplaceAllString(content, `${1}"********"`)
	return envSecretRE.ReplaceAllString(content, "${1}********")
}

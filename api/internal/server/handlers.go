package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/httpx"
	"profanity-filter-api/internal/model"
)

const (
	requestTimeout = 15 * time.Second
	maxBatchTexts  = 100
)

func (a *api) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	snapshot, err := a.store.StatsSnapshot()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not read stats.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func (a *api) handleHistory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 12
		if value := r.URL.Query().Get("limit"); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil {
				limit = parsed
			}
		}
		checks, err := a.store.RecentChecks(limit)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read history.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, checks)
	case http.MethodDelete:
		if !auth.CurrentUser(r).Can(auth.PermModerate) {
			httpx.WriteError(w, http.StatusForbidden, "Your role does not allow this action.")
			return
		}
		if err := a.store.ClearChecks(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not clear history.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET or DELETE required")
	}
}

// handleReviews serves the moderator review queue: GET lists review-band checks
// (pending first), POST records or changes an allow/block decision with an
// optional note, and DELETE reopens an item by clearing its decision.
func (a *api) handleReviews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := a.store.ReviewQueue(100)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not read review queue.")
			return
		}
		pending, err := a.store.PendingReviewCount()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not count pending reviews.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "pending": pending})
	case http.MethodPost:
		var req struct {
			ID       int64  `json:"id"`
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		if err := httpx.DecodeBody(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.ID <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "id is required")
			return
		}
		if req.Decision != "allow" && req.Decision != "block" {
			httpx.WriteError(w, http.StatusBadRequest, "decision must be allow or block")
			return
		}
		if len(req.Note) > 2000 {
			httpx.WriteError(w, http.StatusBadRequest, "note is too long")
			return
		}
		reviewer := auth.CurrentUser(r).Username
		ok, err := a.store.DecideReview(req.ID, req.Decision, strings.TrimSpace(req.Note), reviewer)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not save review decision.")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "Review item not found.")
			return
		}
		slog.Info("review decided", "check", req.ID, "decision", req.Decision, "reviewer", reviewer)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err != nil || id <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "id is required")
			return
		}
		ok, err := a.store.ClearReview(id)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not clear review decision.")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "Review decision not found.")
			return
		}
		slog.Info("review reopened", "check", id, "user", auth.CurrentUser(r).Username)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET, POST or DELETE required")
	}
}

type dbQueryKind int

const (
	dbQueryRead dbQueryKind = iota
	dbQueryDelete
)

var (
	dbDeleteWord    = regexp.MustCompile(`\bDELETE\b`)
	dbForbiddenWord = regexp.MustCompile(`\b(INSERT|UPDATE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE|COPY|VACUUM)\b`)
)

// classifySQL accepts one SELECT/WITH statement, or a DELETE for the admin
// account. Reads still run in a read-only transaction in the store, so this is
// only the first line of defence, not the only one.
func classifySQL(query string) (dbQueryKind, error) {
	trimmed := strings.TrimSpace(query)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return dbQueryRead, fmt.Errorf("query is empty")
	}
	statement := strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	if strings.Contains(statement, ";") {
		return dbQueryRead, fmt.Errorf("only one statement is allowed")
	}
	upper := strings.ToUpper(statement)
	if dbForbiddenWord.MatchString(upper) {
		return dbQueryRead, fmt.Errorf("only SELECT, WITH and DELETE statements are allowed")
	}
	hasDelete := dbDeleteWord.MatchString(upper)
	switch strings.ToUpper(fields[0]) {
	case "SELECT":
		if hasDelete {
			return dbQueryRead, fmt.Errorf("only SELECT, WITH and DELETE statements are allowed")
		}
		return dbQueryRead, nil
	case "DELETE":
		return dbQueryDelete, nil
	case "WITH":
		if hasDelete {
			return dbQueryDelete, nil
		}
		return dbQueryRead, nil
	default:
		return dbQueryRead, fmt.Errorf("only SELECT, WITH and DELETE statements are allowed")
	}
}

func (a *api) handleDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	tables, err := a.store.DBTables()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not read database tables.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

func (a *api) handleDBTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	result, err := a.store.DBTableRows(name)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "Table not found or not readable.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (a *api) handleDBQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		SQL string `json:"sql"`
	}
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := classifySQL(req.SQL)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The route is admin-only; keep the role check here as defence in depth.
	admin := auth.CurrentUser(r).Role == "admin"
	if kind == dbQueryDelete {
		if !admin {
			httpx.WriteError(w, http.StatusForbidden, "Admin access required for DELETE.")
			return
		}
		affected, err := a.store.DBExec(req.SQL)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Info("database delete run", "user", auth.CurrentUser(r).Username, "rows", affected)
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": affected})
		return
	}
	result, err := a.store.DBQuery(req.SQL)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Info("database query run", "user", auth.CurrentUser(r).Username)
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (a *api) handleModerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	res, err := a.worker.Moderate(ctx, req.Text)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := a.store.RecordCheck(req.Text, res); err != nil {
		slog.Error("could not save check", "error", err)
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (a *api) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		Texts []string `json:"texts"`
	}
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Texts) == 0 || len(req.Texts) > maxBatchTexts {
		httpx.WriteError(w, http.StatusBadRequest, fmt.Sprintf("texts must contain 1..%d items", maxBatchTexts))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout*time.Duration(len(req.Texts)))
	defer cancel()

	results := make([]model.ModerationResult, 0, len(req.Texts))
	for _, text := range req.Texts {
		res, err := a.worker.Moderate(ctx, text)
		if err != nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if err := a.store.RecordCheck(text, res); err != nil {
			slog.Error("could not save check", "error", err)
		}
		results = append(results, *res)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": results})
}

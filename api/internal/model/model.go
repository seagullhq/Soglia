// Package model holds the verdict shape shared by the worker client, the API
// handlers and the database store.
package model

// ModerationResult is the verdict returned to callers and persisted in the
// checks table.
type ModerationResult struct {
	Action  string  `json:"action"`
	Tier    string  `json:"tier"`
	Prob    float64 `json:"prob"`
	MS      float64 `json:"ms"`
	Matched string  `json:"matched,omitempty"`
}

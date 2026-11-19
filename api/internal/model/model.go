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

// CalibrationOverride holds optional overrides for the values read from the
// model's calibration.json. A nil field means "use the value from the file".
type CalibrationOverride struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	BlockThreshold  *float64 `json:"block_threshold,omitempty"`
	ReviewThreshold *float64 `json:"review_threshold,omitempty"`
}

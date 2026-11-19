package server

import (
	"math"
	"strings"
	"testing"

	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/model"
)

func floatPtr(value float64) *float64 { return &value }

func boolPtr(value bool) *bool { return &value }

func TestValidateServiceSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings config.AppSettings
		wantErr  string
	}{
		{
			name: "valid defaults",
			settings: config.AppSettings{
				Addr:                ":8080",
				CalibrationOverride: model.CalibrationOverride{Temperature: floatPtr(1.0)},
			},
		},
		{
			name: "valid TLS pair and thresholds",
			settings: config.AppSettings{
				Addr:            "127.0.0.1:8443",
				TLSCert:         "/app/certs/api.crt",
				TLSKey:          "/app/certs/api.key",
				TerminalEnabled: boolPtr(false),
				CalibrationOverride: model.CalibrationOverride{
					Temperature:     floatPtr(1.25),
					BlockThreshold:  floatPtr(0.65),
					ReviewThreshold: floatPtr(0.6),
				},
			},
		},
		{
			name:     "empty address",
			settings: config.AppSettings{},
			wantErr:  "listen address cannot be empty",
		},
		{
			name:     "address without port",
			settings: config.AppSettings{Addr: "8080"},
			wantErr:  "listen address must be host:port",
		},
		{
			name: "certificate without key",
			settings: config.AppSettings{
				Addr:    ":8080",
				TLSCert: "/app/certs/api.crt",
			},
			wantErr: "must be set together",
		},
		{
			name: "zero temperature",
			settings: config.AppSettings{
				Addr:                ":8080",
				CalibrationOverride: model.CalibrationOverride{Temperature: floatPtr(0)},
			},
			wantErr: "temperature must be greater than 0",
		},
		{
			name: "NaN temperature",
			settings: config.AppSettings{
				Addr:                ":8080",
				CalibrationOverride: model.CalibrationOverride{Temperature: floatPtr(math.NaN())},
			},
			wantErr: "temperature must be greater than 0",
		},
		{
			name: "block threshold above one",
			settings: config.AppSettings{
				Addr:                ":8080",
				CalibrationOverride: model.CalibrationOverride{BlockThreshold: floatPtr(1.5)},
			},
			wantErr: "block threshold must be between 0 and 1",
		},
		{
			name: "negative review threshold",
			settings: config.AppSettings{
				Addr:                ":8080",
				CalibrationOverride: model.CalibrationOverride{ReviewThreshold: floatPtr(-0.1)},
			},
			wantErr: "review threshold must be between 0 and 1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateServiceSettings(test.settings)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestTerminalEnabledDefaultsOn(t *testing.T) {
	if !terminalEnabled(config.AppSettings{}) {
		t.Fatal("old settings without terminal_enabled should keep the terminal on")
	}
	if terminalEnabled(config.AppSettings{TerminalEnabled: boolPtr(false)}) {
		t.Fatal("explicit false should disable the terminal")
	}
}

func TestFloatValue(t *testing.T) {
	if got := floatValue(nil, 0.65); got != 0.65 {
		t.Fatalf("nil override: got %v, want 0.65", got)
	}
	if got := floatValue(floatPtr(0.2), 0.65); got != 0.2 {
		t.Fatalf("explicit override: got %v, want 0.2", got)
	}
}

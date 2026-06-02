package logging

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"100", 100, false},
		{"100B", 100, false},
		{"100KB", 100 * 1024, false},
		{"100MB", 100 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100mb", 100 * 1024 * 1024, false}, // case insensitive
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSize(tt.input)
			if tt.hasError && err == nil {
				t.Errorf("parseSize(%q) expected error", tt.input)
			}
			if !tt.hasError && err != nil {
				t.Errorf("parseSize(%q) unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("parseSize(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		hasError bool
	}{
		{"7d", "168h0m0s", false},
		{"1w", "168h0m0s", false},
		{"2w", "336h0m0s", false},
		{"24h", "24h0m0s", false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseDuration(tt.input)
			if tt.hasError && err == nil {
				t.Errorf("parseDuration(%q) expected error", tt.input)
			}
			if !tt.hasError {
				if err != nil {
					t.Errorf("parseDuration(%q) unexpected error: %v", tt.input, err)
				}
				if result.String() != tt.expected {
					t.Errorf("parseDuration(%q) = %v, want %v", tt.input, result.String(), tt.expected)
				}
			}
		})
	}
}

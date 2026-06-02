package logging

import (
	"testing"
)

func TestParseSizeEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"0", 0, false},
		{"0KB", 0, false},
		{"  100MB  ", 100 * 1024 * 1024, false}, // with whitespace
		{"1gb", 1024 * 1024 * 1024, false},      // lowercase
		{"10b", 10, false},                      // bytes lowercase
		{"", 0, true},                           // empty string
		{"-1", -1, false},                       // negative (parsing succeeds)
		{"abc", 0, true},                        // non-numeric
		{"1.5MB", 0, true},                      // float not supported
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
			if !tt.hasError && result != tt.expected {
				t.Errorf("parseSize(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseDurationEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		hasError bool
	}{
		{"1d", "24h0m0s", false},
		{"0d", "0s", false},
		{"30d", "720h0m0s", false},
		{"  7d  ", "168h0m0s", false}, // with whitespace
		{"3w", "504h0m0s", false},     // 3 weeks
		{"1h", "1h0m0s", false},       // standard duration
		{"30m", "30m0s", false},       // minutes
		{"60s", "1m0s", false},        // seconds
		{"", "", true},                // empty string
		{"1x", "", true},              // invalid suffix
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
				} else if result.String() != tt.expected {
					t.Errorf("parseDuration(%q) = %v, want %v", tt.input, result.String(), tt.expected)
				}
			}
		})
	}
}

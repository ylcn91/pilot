package upgrade

import (
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"equal versions", "1.0.0", "1.0.0", 0},
		{"a less than b major", "1.0.0", "2.0.0", -1},
		{"a greater than b major", "2.0.0", "1.0.0", 1},
		{"a less than b minor", "1.0.0", "1.1.0", -1},
		{"a greater than b minor", "1.1.0", "1.0.0", 1},
		{"a less than b patch", "1.0.0", "1.0.1", -1},
		{"a greater than b patch", "1.0.1", "1.0.0", 1},
		{"with v prefix", "v1.0.0", "v1.0.1", -1},
		{"mixed prefix", "v1.0.0", "1.0.1", -1},
		{"with dirty suffix", "1.0.0-dirty", "1.0.1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected [3]int
	}{
		{"simple version", "1.2.3", [3]int{1, 2, 3}},
		{"with v prefix", "v1.2.3", [3]int{1, 2, 3}},
		{"with dirty suffix", "1.2.3-dirty", [3]int{1, 2, 3}},
		{"partial version", "1.2", [3]int{1, 2, 0}},
		{"major only", "1", [3]int{1, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseVersion(tt.version)
			if result != tt.expected {
				t.Errorf("parseVersion(%q) = %v, want %v", tt.version, result, tt.expected)
			}
		})
	}
}

func TestVersionInfo_UpdateAvailable(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"update available", "0.1.0", "0.2.0", true},
		{"no update", "0.2.0", "0.2.0", false},
		{"newer local", "0.3.0", "0.2.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := tt.current
			latest := tt.latest
			got := compareVersions(current, latest) < 0
			if got != tt.want {
				t.Errorf("UpdateAvail = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VersionChecker tests
// ---------------------------------------------------------------------------

func TestNewVersionChecker(t *testing.T) {
	t.Run("default interval", func(t *testing.T) {
		vc := NewVersionChecker("1.0.0", 0)
		if vc.checkInterval != DefaultCheckInterval {
			t.Errorf("checkInterval = %v, want %v", vc.checkInterval, DefaultCheckInterval)
		}
		if vc.currentVersion != "1.0.0" {
			t.Errorf("currentVersion = %q, want %q", vc.currentVersion, "1.0.0")
		}
	})

	t.Run("custom interval", func(t *testing.T) {
		vc := NewVersionChecker("2.0.0", 10*time.Minute)
		if vc.checkInterval != 10*time.Minute {
			t.Errorf("checkInterval = %v, want %v", vc.checkInterval, 10*time.Minute)
		}
	})
}

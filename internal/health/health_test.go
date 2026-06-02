package health

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Status type tests
// ---------------------------------------------------------------------------

func TestStatusSymbol(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOK, "✓"},
		{StatusWarning, "○"},
		{StatusError, "✗"},
		{StatusDisabled, "·"},
		{Status(99), "?"},
	}
	for _, tt := range tests {
		if got := tt.status.Symbol(); got != tt.want {
			t.Errorf("Status(%d).Symbol() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOK, "ok"},
		{StatusWarning, "warning"},
		{StatusError, "error"},
		{StatusDisabled, "disabled"},
		{Status(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestStatusColorSymbol(t *testing.T) {
	// Just verify non-empty and contains the plain symbol
	for _, s := range []Status{StatusOK, StatusWarning, StatusError, StatusDisabled} {
		cs := s.ColorSymbol()
		if cs == "" {
			t.Errorf("Status(%d).ColorSymbol() is empty", s)
		}
		if plain := s.Symbol(); plain != "?" {
			// Colored version should contain the plain symbol rune
			if len(cs) <= len(plain) {
				t.Errorf("Status(%d).ColorSymbol() = %q, expected ANSI-wrapped version", s, cs)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// boolToStatus
// ---------------------------------------------------------------------------

func TestBoolToStatus(t *testing.T) {
	if got := boolToStatus(true); got != StatusOK {
		t.Errorf("boolToStatus(true) = %v, want StatusOK", got)
	}
	if got := boolToStatus(false); got != StatusDisabled {
		t.Errorf("boolToStatus(false) = %v, want StatusDisabled", got)
	}
}

// ---------------------------------------------------------------------------
// expandPath
// ---------------------------------------------------------------------------

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"tilde prefix", "~/projects", filepath.Join(home, "projects")},
		{"tilde only", "~", filepath.Join(home)},
		{"absolute", "/usr/local/bin", "/usr/local/bin"},
		{"relative", "foo/bar", "foo/bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandPath(tt.path); got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HealthReport.Summary
// ---------------------------------------------------------------------------

func TestHealthReportSummary(t *testing.T) {
	report := &HealthReport{
		Dependencies: []Check{
			{Name: "git", Status: StatusOK},
			{Name: "claude", Status: StatusError},
			{Name: "gh", Status: StatusWarning},
		},
		Config: []ConfigCheck{
			{Name: "config file", Status: StatusOK},
			{Name: "telegram", Status: StatusError},
			{Name: "projects", Status: StatusWarning},
		},
	}

	errors, warnings := report.Summary()
	if errors != 2 {
		t.Errorf("Summary() errors = %d, want 2", errors)
	}
	if warnings != 2 {
		t.Errorf("Summary() warnings = %d, want 2", warnings)
	}
}

func TestHealthReportSummaryClean(t *testing.T) {
	report := &HealthReport{
		Dependencies: []Check{{Name: "git", Status: StatusOK}},
		Config:       []ConfigCheck{{Name: "config", Status: StatusOK}},
	}

	errors, warnings := report.Summary()
	if errors != 0 || warnings != 0 {
		t.Errorf("Summary() = (%d, %d), want (0, 0)", errors, warnings)
	}
}

// ---------------------------------------------------------------------------
// HealthReport.ReadyToStart
// ---------------------------------------------------------------------------

func TestReadyToStart(t *testing.T) {
	tests := []struct {
		name string
		deps []Check
		want bool
	}{
		{
			name: "all ok",
			deps: []Check{
				{Name: "claude", Status: StatusOK, Message: "1.0.0 [active backend]"},
				{Name: "git", Status: StatusOK},
			},
			want: true,
		},
		{
			name: "active backend missing",
			deps: []Check{
				{Name: "claude", Status: StatusError, Message: "not found [active backend]"},
				{Name: "git", Status: StatusOK},
			},
			want: false,
		},
		{
			name: "non-active backend missing is ok",
			deps: []Check{
				{Name: "claude", Status: StatusOK, Message: "1.0.0 [active backend]"},
				{Name: "qwen", Status: StatusError, Message: "not found"},
				{Name: "git", Status: StatusOK},
			},
			want: true,
		},
		{
			name: "git missing",
			deps: []Check{
				{Name: "claude", Status: StatusOK, Message: "1.0.0 [active backend]"},
				{Name: "git", Status: StatusError},
			},
			want: false,
		},
		{
			name: "gh warning is ok",
			deps: []Check{
				{Name: "claude", Status: StatusOK, Message: "1.0.0 [active backend]"},
				{Name: "git", Status: StatusOK},
				{Name: "gh", Status: StatusWarning},
			},
			want: true,
		},
		{
			name: "empty deps",
			deps: []Check{},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &HealthReport{Dependencies: tt.deps}
			if got := report.ReadyToStart(); got != tt.want {
				t.Errorf("ReadyToStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

package executor

import (
	"strings"
	"testing"
)

func TestSetPRCreator(t *testing.T) {
	runner := NewRunner()
	if runner.prCreator != nil {
		t.Error("prCreator should be nil by default")
	}
	mock := &mockPRCreator{}
	runner.SetPRCreator(mock)
	if runner.prCreator == nil {
		t.Fatal("prCreator should be set after SetPRCreator")
	}
}

func TestPRCreator_RoutingCondition(t *testing.T) {
	runner := NewRunner()
	mock := &mockPRCreator{ReturnURL: "https://gitlab.com/ns/proj/-/merge_requests/1"}
	runner.SetPRCreator(mock)
	task := &Task{ID: "GL-1", SourceAdapter: "gitlab"}
	useAdapter := runner.prCreator != nil && task.SourceAdapter != "" && task.SourceAdapter != "github"
	if !useAdapter {
		t.Error("should use PRCreator for gitlab adapter")
	}
}

func TestPRCreator_NotUsedForGitHubAdapter(t *testing.T) {
	runner := NewRunner()
	runner.SetPRCreator(&mockPRCreator{})
	task := &Task{SourceAdapter: "github"}
	useAdapter := runner.prCreator != nil && task.SourceAdapter != "" && task.SourceAdapter != "github"
	if useAdapter {
		t.Error("should NOT use PRCreator for github adapter")
	}
}

func TestPRCreator_NotUsedWhenEmpty(t *testing.T) {
	runner := NewRunner()
	runner.SetPRCreator(&mockPRCreator{})
	task := &Task{SourceAdapter: ""}
	useAdapter := runner.prCreator != nil && task.SourceAdapter != "" && task.SourceAdapter != "github"
	if useAdapter {
		t.Error("should NOT use PRCreator when SourceAdapter is empty")
	}
}

// TestTruncateDiagnostic verifies the diagnostic truncation helper used by
// persistBackendDiagnostics honors its character ceilings and appends a
// visible marker so readers know the payload was clipped. GH-2328.
func TestTruncateDiagnostic(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		max     int
		want    string
		trunced bool
	}{
		{
			name:  "under limit unchanged",
			input: "short stderr",
			max:   100,
			want:  "short stderr",
		},
		{
			name:  "equal to limit unchanged",
			input: strings.Repeat("x", 10),
			max:   10,
			want:  strings.Repeat("x", 10),
		},
		{
			name:    "over limit truncated with marker",
			input:   strings.Repeat("y", 20),
			max:     5,
			want:    strings.Repeat("y", 5) + "\n[...truncated]",
			trunced: true,
		},
		{
			name:    "stderr ceiling (16 KB)",
			input:   strings.Repeat("e", 20*1024),
			max:     diagnosticsStderrMaxChars,
			trunced: true,
		},
		{
			name:    "message ceiling (4 KB)",
			input:   strings.Repeat("m", 10*1024),
			max:     diagnosticsMessageMaxChars,
			trunced: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDiagnostic(tt.input, tt.max)
			if !tt.trunced && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if tt.trunced {
				if !strings.HasSuffix(got, "\n[...truncated]") {
					t.Errorf("expected truncation marker, got %q", got[max(0, len(got)-32):])
				}
				// Output length = max + len marker.
				expectedLen := tt.max + len("\n[...truncated]")
				if len(got) != expectedLen {
					t.Errorf("truncated length = %d, want %d", len(got), expectedLen)
				}
			}
		})
	}

	// Ceiling constants must match the design target so project-side tooling
	// that assumes ≤16 KB / ≤4 KB keeps working. GH-2328 acceptance.
	if diagnosticsStderrMaxChars != 16*1024 {
		t.Errorf("diagnosticsStderrMaxChars = %d, want 16384", diagnosticsStderrMaxChars)
	}
	if diagnosticsMessageMaxChars != 4*1024 {
		t.Errorf("diagnosticsMessageMaxChars = %d, want 4096", diagnosticsMessageMaxChars)
	}
}

// GH-2402: IsPermanentFailure must classify deterministic errors so the
// caller can apply pilot-blocked instead of pilot-failed (which auto-retries).
func TestIsPermanentFailure(t *testing.T) {
	tests := []struct {
		name string
		err  string
		want bool
	}{
		{"empty string", "", false},
		{"transient network error", "connection refused: dial tcp", false},
		{"rate limit hit", "rate limit exceeded, retry after 60s", false},
		{"non-conventional title", "PR creation refused: title is not a conventional commit: 'Implement test thing'", true},
		// GH-2735: "could not auto-correct" removed from permanentFailurePatterns; normalizeTitle now always produces a valid title.
		{"could not auto-correct title", "title is invalid: could not auto-correct to a conventional commit", false},
		{"PR creation refused (catch-all)", "PR creation refused: some reason", true},
		{"random failure", "no_changes: Claude completed but made no code changes", false},
		// GH-3224: no-op runs (ghost-SHA guard) are deterministic — terminal.
		{"no-op worktree HEAD", "no new commit produced — worktree HEAD matches base branch parent", true},
		{"no-op post-push SHA", "no new commit produced — post-push SHA matches base branch", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPermanentFailure(tt.err); got != tt.want {
				t.Errorf("IsPermanentFailure(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

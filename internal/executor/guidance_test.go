package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGuidancePreambleContainsHeader(t *testing.T) {
	out := BuildGuidancePreamble(t.TempDir(), "add github webhook handler")

	if !strings.Contains(out, "[PILOT-EXEC]") {
		t.Errorf("expected preamble to contain executor header marker, got:\n%s", out)
	}
	if !strings.Contains(out, "WORKFLOW CHECK") {
		t.Errorf("expected preamble to contain workflow instructions, got:\n%s", out)
	}
}

func TestBuildGuidancePreambleIncludesRelevantSOP(t *testing.T) {
	agentDir := t.TempDir()
	sopsDir := filepath.Join(agentDir, "sops")
	if err := os.MkdirAll(sopsDir, 0o755); err != nil {
		t.Fatalf("failed to create sops dir: %v", err)
	}
	// Filename must contain a keyword that findRelevantSOPs extracts from the
	// task description; "github" qualifies.
	sopPath := filepath.Join(sopsDir, "github-api.md")
	if err := os.WriteFile(sopPath, []byte("GitHub API SOP"), 0o644); err != nil {
		t.Fatalf("failed to write sop: %v", err)
	}

	out := BuildGuidancePreamble(agentDir, "add github webhook handler")

	if !strings.Contains(out, "Relevant SOPs") {
		t.Errorf("expected preamble to contain SOP section, got:\n%s", out)
	}
	if !strings.Contains(out, "sops/github-api.md") {
		t.Errorf("expected preamble to reference the github SOP, got:\n%s", out)
	}
}

func TestBuildGuidancePreambleEmptyAgentDir(t *testing.T) {
	// A missing/empty .agent dir must not panic and must still return the
	// static header + workflow pieces.
	out := BuildGuidancePreamble(filepath.Join(t.TempDir(), "does-not-exist"), "")

	if out == "" {
		t.Fatal("expected non-empty preamble from static pieces")
	}
	if !strings.Contains(out, "[PILOT-EXEC]") {
		t.Errorf("expected header marker even with empty agent dir, got:\n%s", out)
	}
	if strings.Contains(out, "Relevant SOPs") {
		t.Errorf("did not expect SOP section with empty agent dir, got:\n%s", out)
	}
}

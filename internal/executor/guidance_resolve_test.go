package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGuidanceAtom writes <agentDir>/executor-guidance/<key>.md with the given
// body and returns agentDir. mode is written into YAML frontmatter; pass "" to
// emit a body with no frontmatter at all.
func writeGuidanceAtom(t *testing.T, key, mode, body string) string {
	t.Helper()
	agentDir := t.TempDir()
	dir := filepath.Join(agentDir, "executor-guidance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir executor-guidance: %v", err)
	}

	var content string
	if mode == "" {
		content = body
	} else {
		content = "---\nmode: " + mode + "\n---\n\n" + body
	}
	if err := os.WriteFile(filepath.Join(dir, key+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write atom: %v", err)
	}
	return agentDir
}

// TestLoadGuidanceAbsentFileVerbatim is the byte-identity guarantee: with no
// executor-guidance/<key>.md present, loadGuidance returns the fallback const
// exactly so existing prompts stay unchanged.
func TestLoadGuidanceAbsentFileVerbatim(t *testing.T) {
	fallback := "## Original Const\n\nbody line\n"

	// agentDir == "" path.
	if got := loadGuidance("", "workflow", fallback); got != fallback {
		t.Errorf("empty agentDir: got %q, want fallback verbatim", got)
	}

	// agentDir present but no executor-guidance dir.
	if got := loadGuidance(t.TempDir(), "workflow", fallback); got != fallback {
		t.Errorf("missing file: got %q, want fallback verbatim", got)
	}

	// executor-guidance dir present but the key file absent.
	agentDir := writeGuidanceAtom(t, "workflow", "overlay", "x")
	if got := loadGuidance(agentDir, "pre-commit", fallback); got != fallback {
		t.Errorf("absent key: got %q, want fallback verbatim", got)
	}
}

// TestLoadGuidanceOverlayAppends: overlay mode emits the const first, then the
// file body, separated by a blank line.
func TestLoadGuidanceOverlayAppends(t *testing.T) {
	fallback := "## Const\n\nconst body"
	body := "## Refinement\n\nextra rule"
	agentDir := writeGuidanceAtom(t, "workflow", "overlay", body)

	got := loadGuidance(agentDir, "workflow", fallback)
	want := fallback + "\n\n" + body
	if got != want {
		t.Errorf("overlay mismatch:\n got %q\nwant %q", got, want)
	}
	if !strings.HasPrefix(got, fallback) {
		t.Error("overlay must lead with the const")
	}
}

// TestLoadGuidanceOverlayWhenModeMissing: absent/unrecognized mode falls back to
// overlay (the conservative default — const is never silently dropped).
func TestLoadGuidanceOverlayWhenModeMissing(t *testing.T) {
	fallback := "const body"
	body := "appended body"

	// No frontmatter at all.
	noFM := writeGuidanceAtom(t, "workflow", "", body)
	if got := loadGuidance(noFM, "workflow", fallback); got != fallback+"\n\n"+body {
		t.Errorf("missing frontmatter should overlay, got %q", got)
	}

	// Unrecognized mode value.
	bogus := writeGuidanceAtom(t, "workflow", "bogus", body)
	if got := loadGuidance(bogus, "workflow", fallback); got != fallback+"\n\n"+body {
		t.Errorf("unrecognized mode should overlay, got %q", got)
	}
}

// TestLoadGuidanceOverrideReplaces: override mode on a non-protected key returns
// only the file body — nothing of the const survives.
func TestLoadGuidanceOverrideReplaces(t *testing.T) {
	fallback := "## Const\n\nconst body that must vanish"
	body := "## Replacement\n\nfull replacement body"
	agentDir := writeGuidanceAtom(t, "workflow", "override", body)

	got := loadGuidance(agentDir, "workflow", fallback)
	if got != body {
		t.Errorf("override should return body only, got %q", got)
	}
	if strings.Contains(got, "const body that must vanish") {
		t.Error("override must not retain the const")
	}
}

// TestLoadGuidanceOverrideForcedToOverlayForProtectedKeys: header and
// evidence-spec are overlay-only. An override on them is downgraded to overlay
// so the [PILOT-EXEC] sentinel / NO-OP guard is never dropped.
func TestLoadGuidanceOverrideForcedToOverlayForProtectedKeys(t *testing.T) {
	for _, key := range []string{"header", "evidence-spec"} {
		t.Run(key, func(t *testing.T) {
			fallback := "[SENTINEL] structural const for " + key
			body := "local addition"
			agentDir := writeGuidanceAtom(t, key, "override", body)

			got := loadGuidance(agentDir, key, fallback)
			if !strings.HasPrefix(got, fallback) {
				t.Errorf("protected key %s must keep the const ahead of the body, got %q", key, got)
			}
			if got != fallback+"\n\n"+body {
				t.Errorf("protected key %s should overlay despite override, got %q", key, got)
			}
		})
	}
}

// TestLoadGuidanceBudgetTruncation: a body past guidanceFileBudget is truncated
// on a line boundary (the warn is emitted as a side effect; we assert the cut).
func TestLoadGuidanceBudgetTruncation(t *testing.T) {
	// Build a body well over budget out of fixed-length lines so the cut lands
	// on a newline boundary.
	line := strings.Repeat("a", 99) + "\n" // 100 chars/line
	body := strings.Repeat(line, (guidanceFileBudget/100)+50)
	agentDir := writeGuidanceAtom(t, "workflow", "override", body)

	got := loadGuidance(agentDir, "workflow", "fallback")
	if len(got) > guidanceFileBudget {
		t.Errorf("truncated body should be within budget, got %d chars", len(got))
	}
	if len(got) == 0 {
		t.Fatal("truncated body should be non-empty")
	}
	// Cut on a line boundary: the last retained char is the end of a full line,
	// so no partial 'a'-run shorter than 99 should trail.
	if strings.HasSuffix(got, "\n") {
		t.Error("trailing newline should be trimmed after truncation")
	}
	lines := strings.Split(got, "\n")
	if last := lines[len(lines)-1]; len(last) != 99 {
		t.Errorf("truncation should land on a line boundary, last line len=%d", len(last))
	}
}

// TestLoadGuidanceEmptyBodyFallsBack: a file whose body is empty after frontmatter
// stripping yields the fallback (an empty overlay must not blank the prompt).
func TestLoadGuidanceEmptyBodyFallsBack(t *testing.T) {
	fallback := "## Const\n\nconst body"
	agentDir := writeGuidanceAtom(t, "workflow", "override", "   \n\n  ")
	if got := loadGuidance(agentDir, "workflow", fallback); got != fallback {
		t.Errorf("empty body should fall back to const, got %q", got)
	}
}

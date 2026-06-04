package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecuteToolWorkspaceConfinement guards the direct-API tool runner: file
// tools must stay inside the workspace and catastrophic bash is blocked, while
// in-workspace operations still work.
func TestExecuteToolWorkspaceConfinement(t *testing.T) {
	cwd := t.TempDir()

	t.Run("write inside workspace succeeds", func(t *testing.T) {
		out := executeTool("write_file", map[string]any{"path": "sub/a.txt", "content": "hi"}, cwd)
		if strings.Contains(out, "BLOCKED") {
			t.Fatalf("in-workspace write blocked: %s", out)
		}
		if _, err := os.Stat(filepath.Join(cwd, "sub", "a.txt")); err != nil {
			t.Errorf("file not written: %v", err)
		}
	})

	t.Run("absolute path outside workspace is blocked", func(t *testing.T) {
		out := executeTool("write_file", map[string]any{"path": "/etc/pilot-escape-test", "content": "x"}, cwd)
		if !strings.Contains(out, "BLOCKED") {
			t.Errorf("escape not blocked: %s", out)
		}
	})

	t.Run("parent traversal is blocked", func(t *testing.T) {
		out := executeTool("write_file", map[string]any{"path": "../escape.txt", "content": "x"}, cwd)
		if !strings.Contains(out, "BLOCKED") {
			t.Errorf("traversal not blocked: %s", out)
		}
	})

	t.Run("read outside workspace is blocked", func(t *testing.T) {
		out := executeTool("read_file", map[string]any{"path": "/etc/hosts"}, cwd)
		if !strings.Contains(out, "BLOCKED") {
			t.Errorf("read escape not blocked: %s", out)
		}
	})

	t.Run("fork bomb is blocked", func(t *testing.T) {
		out := executeTool("bash", map[string]any{"command": ":(){ :|:& };:"}, cwd)
		if !strings.Contains(out, "BLOCKED") {
			t.Errorf("fork bomb not blocked: %s", out)
		}
	})

	t.Run("ordinary bash still runs", func(t *testing.T) {
		out := executeTool("bash", map[string]any{"command": "echo hello"}, cwd)
		if strings.Contains(out, "BLOCKED") {
			t.Fatalf("ordinary command blocked: %s", out)
		}
		if !strings.Contains(out, "hello") {
			t.Errorf("expected command output, got %q", out)
		}
	})
}

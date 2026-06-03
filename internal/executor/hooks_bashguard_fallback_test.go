package executor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// coreutilsPathWithoutJQ builds a directory of symlinks to the real binaries the
// bash-guard fallback needs (cat, grep, sed, bash, tr) resolved to absolute
// paths, deliberately omitting jq. Returning a PATH that contains ONLY this
// directory forces `command -v jq` inside the script to fail, exercising the
// grep/sed extraction fallback path. It skips the test if any required binary
// cannot be resolved to an absolute executable.
func coreutilsPathWithoutJQ(t *testing.T) (binDir string, shell string) {
	t.Helper()
	binDir = t.TempDir()
	// bash is the script interpreter; the rest are used inside the script.
	required := []string{"bash", "cat", "grep", "sed", "tr", "wc"}
	for _, name := range required {
		// exec.LookPath resolves against the ambient PATH and returns an absolute
		// path (avoids shell-builtin/alias resolution that returns a bare name).
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("required binary %q not found on PATH: %v", name, err)
		}
		abs, err := filepath.Abs(p)
		if err != nil || !filepath.IsAbs(abs) {
			t.Skipf("could not resolve absolute path for %q (got %q, err %v)", name, p, err)
		}
		if name == "bash" {
			shell = abs
		}
		if err := os.Symlink(abs, filepath.Join(binDir, name)); err != nil {
			t.Skipf("symlink %q: %v", name, err)
		}
	}
	return binDir, shell
}

// extractBashGuardScript writes the embedded pilot-bash-guard.sh to a temp file
// and returns its path.
func extractBashGuardScript(t *testing.T) string {
	t.Helper()
	content, err := embeddedHookScripts.ReadFile("hookscripts/pilot-bash-guard.sh")
	if err != nil {
		t.Fatalf("read embedded bash-guard script: %v", err)
	}
	path := filepath.Join(t.TempDir(), "pilot-bash-guard.sh")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write bash-guard script: %v", err)
	}
	return path
}

// TestBashGuardJQFallback exercises the pilot-bash-guard.sh grep/sed extraction
// path that runs when jq is NOT on PATH. The script must still extract the
// command from the JSON stdin and block destructive patterns, falling back to
// the hand-rolled JSON output (no jq) for the deny decision.
func TestBashGuardJQFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-guard relies on a POSIX shell; skipping on windows")
	}

	binDir, shell := coreutilsPathWithoutJQ(t)
	script := extractBashGuardScript(t)

	tests := []struct {
		name      string
		stdin     string
		wantDeny  bool
		wantInOut string // substring expected in stdout when denied
	}{
		{
			name:      "destructive git push --force is blocked via fallback",
			stdin:     `{"tool_input":{"command":"git push --force origin main"}}`,
			wantDeny:  true,
			wantInOut: "git push --force",
		},
		{
			name:     "destructive rm -rf / is blocked via fallback",
			stdin:    `{"tool_input":{"command":"rm -rf /"}}`,
			wantDeny: true,
			// The matched pattern is reported in the deny reason.
			wantInOut: "permissionDecision",
		},
		{
			name:     "safe command is allowed via fallback",
			stdin:    `{"tool_input":{"command":"go build ./..."}}`,
			wantDeny: false,
		},
		{
			name:     "empty command is allowed via fallback",
			stdin:    `{"tool_input":{}}`,
			wantDeny: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(shell, script)
			// PATH contains ONLY the coreutils dir (no jq) so the script takes the
			// jq-unavailable branch.
			cmd.Env = []string{"PATH=" + binDir}
			cmd.Stdin = strings.NewReader(tt.stdin)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			// The script always exits 0 (deny is expressed via JSON, not exit code).
			if err != nil {
				t.Fatalf("guard script exited non-zero: %v\nstderr: %s", err, stderr.String())
			}

			// Sanity: stderr must not complain about missing core tools — that would
			// mean the fallback extraction silently no-op'd rather than truly running.
			if strings.Contains(stderr.String(), "command not found") {
				t.Fatalf("a required core tool was missing from the isolated PATH; stderr:\n%s", stderr.String())
			}

			out := stdout.String()
			denied := strings.Contains(out, `"permissionDecision":"deny"`)
			if denied != tt.wantDeny {
				t.Fatalf("deny=%v, want %v\nstdout: %q", denied, tt.wantDeny, out)
			}
			if tt.wantDeny && tt.wantInOut != "" && !strings.Contains(out, tt.wantInOut) {
				t.Errorf("deny output missing %q\nstdout: %q", tt.wantInOut, out)
			}
			if !tt.wantDeny && strings.TrimSpace(out) != "" {
				t.Errorf("safe command should produce no output, got %q", out)
			}
		})
	}
}

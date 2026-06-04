package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// initGitRepoWithCommit creates a git repo at dir with a single commit and
// returns its HEAD SHA. Used to give the worktree and the daemon CWD distinct
// HEADs so the #18 regression test can tell which one cmd.Dir resolved against.
func initGitRepoWithCommit(t *testing.T, dir, content string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", content)

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestGetPostExecutionSummary_RunsInWorktree is the #18 regression: the claude
// subprocess (which runs git internally) must be pinned to the issue worktree
// via cmd.Dir, not the daemon's CWD. The fake claude echoes the HEAD SHA of
// whatever directory it runs in; the test asserts the harvested SHA matches the
// worktree, not the CWD it was launched from.
func TestGetPostExecutionSummary_RunsInWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub requires a POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	worktree := t.TempDir()
	cwd := t.TempDir()
	worktreeSHA := initGitRepoWithCommit(t, worktree, "worktree-commit")
	cwdSHA := initGitRepoWithCommit(t, cwd, "cwd-commit")
	if worktreeSHA == cwdSHA {
		t.Fatalf("worktree and cwd SHAs collided (%s) — test cannot distinguish", worktreeSHA)
	}

	// Fake claude: resolve HEAD in its own CWD and emit the Claude Code
	// --json-schema wrapper shape that extractStructuredOutput expects.
	stub := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"sha=$(git rev-parse HEAD)\n" +
		`printf '{"result":"ok","session_id":"s","structured_output":{"branch_name":"main","commit_sha":"%s"}}\n' "$sha"` + "\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// Launch the process from the daemon CWD to prove cmd.Dir, not the inherited
	// working directory, decides which repo git resolves against.
	origWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir cwd: %v", err)
	}

	r := &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{Command: stub},
		},
	}

	summary, err := r.getPostExecutionSummary(context.Background(), worktree)
	if err != nil {
		t.Fatalf("getPostExecutionSummary: %v", err)
	}
	if summary.CommitSHA != worktreeSHA {
		t.Fatalf("commit_sha = %q, want worktree SHA %q (got cwd SHA? %v)",
			summary.CommitSHA, worktreeSHA, summary.CommitSHA == cwdSHA)
	}
}

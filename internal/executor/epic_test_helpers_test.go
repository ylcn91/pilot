package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// writeMockScript creates a temporary executable script that outputs the given text
// and exits with the given code. Returns the path to the script.
func writeMockScript(t *testing.T, dir, output string, exitCode int) string {
	t.Helper()
	scriptPath := filepath.Join(dir, "mock-claude")
	script := "#!/bin/sh\n"
	if output != "" {
		script += "cat <<'ENDOFOUTPUT'\n" + output + "\nENDOFOUTPUT\n"
	}
	script += "exit " + fmt.Sprintf("%d", exitCode) + "\n"
	err := os.WriteFile(scriptPath, []byte(script), 0o755)
	if err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}
	return scriptPath
}

// newTestRunner creates a Runner with a mock Claude command for testing PlanEpic.
func newTestRunner(claudeCmd string) *Runner {
	return &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: claudeCmd,
			},
		},
		running:           make(map[string]*exec.Cmd),
		progressCallbacks: make(map[string]ProgressCallback),
		tokenCallbacks:    make(map[string]TokenCallback),
		log:               slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		modelRouter:       NewModelRouter(nil, nil),
	}
}

// mockSubIssueCreator is a mock implementation of SubIssueCreator for testing.
type mockSubIssueCreator struct {
	// Called tracks CreateIssue calls
	Called []mockCreateIssueCall
	// Returns configures what CreateIssue returns
	Returns []mockCreateIssueReturn
	// CurrentCall tracks which call we're on
	CurrentCall int
}

type mockCreateIssueCall struct {
	ParentID string
	Title    string
	Body     string
	Labels   []string
}

type mockCreateIssueReturn struct {
	Identifier string
	URL        string
	Err        error
}

func (m *mockSubIssueCreator) CreateIssue(ctx context.Context, parentID, title, body string, labels []string) (string, string, error) {
	m.Called = append(m.Called, mockCreateIssueCall{
		ParentID: parentID,
		Title:    title,
		Body:     body,
		Labels:   labels,
	})

	if m.CurrentCall >= len(m.Returns) {
		return "", "", fmt.Errorf("unexpected call to CreateIssue")
	}

	ret := m.Returns[m.CurrentCall]
	m.CurrentCall++
	return ret.Identifier, ret.URL, ret.Err
}

// mockSubIssueLinker records LinkSubIssue calls for test assertions.
type mockSubIssueLinker struct {
	mu    sync.Mutex
	Calls []mockLinkSubIssueCall
	ErrFn func(owner, repo string, parentNum, childNum int) error // optional error injection
}

type mockLinkSubIssueCall struct {
	Owner     string
	Repo      string
	ParentNum int
	ChildNum  int
}

func (m *mockSubIssueLinker) LinkSubIssue(_ context.Context, owner, repo string, parentNum, childNum int) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, mockLinkSubIssueCall{owner, repo, parentNum, childNum})
	m.mu.Unlock()
	if m.ErrFn != nil {
		return m.ErrFn(owner, repo, parentNum, childNum)
	}
	return nil
}

// staticAllowlist is a test helper that allows a fixed set of "owner/repo"
// pairs. projectPath comparison is ignored (tests don't need that dimension).
type staticAllowlist struct {
	repos []string // "owner/repo"
}

func (s *staticAllowlist) RepoIsAllowed(owner, repo, projectPath string) bool {
	want := owner + "/" + repo
	for _, r := range s.repos {
		if r == want {
			return true
		}
	}
	return false
}

func (s *staticAllowlist) ConfiguredRepos() []string { return s.repos }

func makeAllowedGitHubWorktree(t *testing.T, ownerRepo string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	worktree := t.TempDir()
	runGitForGuardrail(t, worktree, "init", "-q")
	runGitForGuardrail(t, worktree, "remote", "add", "origin", "https://github.com/"+ownerRepo+".git")
	return worktree
}

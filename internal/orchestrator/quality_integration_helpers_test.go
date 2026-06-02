package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

// setupTestGitRepo creates a temporary git repository for testing.
// The repo is initialized with a single commit so pre-flight checks pass.
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run()

	// Create initial commit so the repo is in a clean state
	testFile := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "initial commit").Run()

	return tmpDir
}

// mockQualityChecker implements executor.QualityChecker for testing
type mockQualityChecker struct {
	outcomes []*executor.QualityOutcome // Sequential outcomes for multiple calls
	callIdx  int32                      // Atomic counter for thread-safe access
	err      error
}

func (m *mockQualityChecker) Check(ctx context.Context) (*executor.QualityOutcome, error) {
	if m.err != nil {
		return nil, m.err
	}
	idx := atomic.AddInt32(&m.callIdx, 1) - 1
	if int(idx) >= len(m.outcomes) {
		// Return last outcome if we've exhausted the list
		return m.outcomes[len(m.outcomes)-1], nil
	}
	return m.outcomes[idx], nil
}

func (m *mockQualityChecker) callCount() int {
	return int(atomic.LoadInt32(&m.callIdx))
}

// mockBackend implements executor.Backend for testing
type mockBackend struct {
	name            string
	execResults     []*executor.BackendResult // Results to return for each Execute call
	execIdx         int32
	execErr         error
	capturedPrompts []string // Capture prompts for verification
	mu              sync.Mutex
}

func (m *mockBackend) Name() string {
	return m.name
}

func (m *mockBackend) Execute(ctx context.Context, opts executor.ExecuteOptions) (*executor.BackendResult, error) {
	// Capture prompt
	m.mu.Lock()
	m.capturedPrompts = append(m.capturedPrompts, opts.Prompt)
	m.mu.Unlock()

	if m.execErr != nil {
		return nil, m.execErr
	}
	idx := atomic.AddInt32(&m.execIdx, 1) - 1
	if int(idx) >= len(m.execResults) {
		return m.execResults[len(m.execResults)-1], nil
	}
	return m.execResults[idx], nil
}

func (m *mockBackend) IsAvailable() bool {
	return true
}

func (m *mockBackend) execCount() int {
	return int(atomic.LoadInt32(&m.execIdx))
}

func (m *mockBackend) getPrompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.capturedPrompts))
	copy(result, m.capturedPrompts)
	return result
}

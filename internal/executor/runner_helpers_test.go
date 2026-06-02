package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

// mockTeamChecker implements TeamChecker for testing
type mockTeamChecker struct {
	permErr    error  // Error to return from CheckPermission
	accessErr  error  // Error to return from CheckProjectAccess
	lastPerm   string // Last permission checked
	lastMember string // Last member ID checked
}

func (m *mockTeamChecker) CheckPermission(memberID string, perm string) error {
	m.lastMember = memberID
	m.lastPerm = perm
	return m.permErr
}

func (m *mockTeamChecker) CheckProjectAccess(memberID, projectPath string, requiredPerm string) error {
	m.lastMember = memberID
	m.lastPerm = requiredPerm
	return m.accessErr
}

// mockSelfReviewBackend implements Backend for self-review tests.
type mockSelfReviewBackend struct {
	output string
}

func (m *mockSelfReviewBackend) Name() string      { return "mock" }
func (m *mockSelfReviewBackend) IsAvailable() bool { return true }
func (m *mockSelfReviewBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	return &BackendResult{Success: true, Output: m.output}, nil
}

// mockSelfReviewExtractor implements SelfReviewExtractor for testing.
type mockSelfReviewExtractor struct {
	mu           sync.Mutex
	extractCalls int
	saveCalls    int
	lastResult   *memory.ExtractionResult
	extractFunc  func(ctx context.Context, output string, projectPath string) (*memory.ExtractionResult, error)
}

func (m *mockSelfReviewExtractor) ExtractFromSelfReview(ctx context.Context, output string, projectPath string) (*memory.ExtractionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extractCalls++
	if m.extractFunc != nil {
		return m.extractFunc(ctx, output, projectPath)
	}
	return &memory.ExtractionResult{}, nil
}

func (m *mockSelfReviewExtractor) SaveExtractedPatterns(ctx context.Context, result *memory.ExtractionResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	m.lastResult = result
	return nil
}

// mockPRCreator records calls to CreatePR for testing.
type mockPRCreator struct {
	Called       bool
	SourceBranch string
	TargetBranch string
	Title        string
	Body         string
	ReturnURL    string
	ReturnErr    error
}

func (m *mockPRCreator) CreatePR(_ context.Context, sourceBranch, targetBranch, title, body string) (string, error) {
	m.Called = true
	m.SourceBranch = sourceBranch
	m.TargetBranch = targetBranch
	m.Title = title
	m.Body = body
	return m.ReturnURL, m.ReturnErr
}

// mockFixedBackend returns a fixed BackendResult for every Execute call and tracks count.
type mockFixedBackend struct {
	mu        sync.Mutex
	result    *BackendResult
	execCount int
}

func (m *mockFixedBackend) Name() string      { return "mock-fixed" }
func (m *mockFixedBackend) IsAvailable() bool { return true }
func (m *mockFixedBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCount++
	return m.result, nil
}

// setupPRGuardRepo creates a temp git repo with an initial commit on main and
// checks out the given branch. If addCommit is true, one additional commit is
// added on the branch (simulating real work).
func setupPRGuardRepo(t *testing.T, branch string, addCommit bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, err := os.MkdirTemp("", "pilot-pr-guard-*")
	if err != nil {
		t.Fatalf("setupPRGuardRepo: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@pilot.local")
	run("config", "user.name", "Pilot Test")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatalf("setupPRGuardRepo: WriteFile: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")
	run("checkout", "-b", branch)

	if addCommit {
		if err := os.WriteFile(filepath.Join(dir, "change.go"), []byte("package main\n"), 0644); err != nil {
			t.Fatalf("setupPRGuardRepo: WriteFile change.go: %v", err)
		}
		run("add", ".")
		run("commit", "-m", "fix(executor): implement change")
	}
	return dir
}

// mockSequentialBackend returns different results per call index. GH-2777.
type mockSequentialBackend struct {
	mu      sync.Mutex
	results []*BackendResult
	idx     int
}

func (m *mockSequentialBackend) Name() string      { return "mock-sequential" }
func (m *mockSequentialBackend) IsAvailable() bool { return true }
func (m *mockSequentialBackend) Execute(_ context.Context, _ ExecuteOptions) (*BackendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.results) {
		return m.results[len(m.results)-1], nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

// fakeMetricsRecorder captures calls to the MetricsRecorder interface for assertions.
type fakeMetricsRecorder struct {
	mu         sync.Mutex
	tokenCalls []struct {
		model, direction string
		n                int64
	}
	costCalls []struct {
		model   string
		costUSD float64
	}
	execCalls     []struct{ model, result string }
	durationCalls []time.Duration
}

func (f *fakeMetricsRecorder) RecordTokens(model, direction string, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenCalls = append(f.tokenCalls, struct {
		model, direction string
		n                int64
	}{model, direction, n})
}

func (f *fakeMetricsRecorder) RecordCost(model string, costUSD float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.costCalls = append(f.costCalls, struct {
		model   string
		costUSD float64
	}{model, costUSD})
}

func (f *fakeMetricsRecorder) RecordExecution(model, result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execCalls = append(f.execCalls, struct{ model, result string }{model, result})
}

func (f *fakeMetricsRecorder) RecordExecutionDuration(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.durationCalls = append(f.durationCalls, d)
}

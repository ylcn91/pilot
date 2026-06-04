package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// --- GH-1955: Self-review pattern extraction ---

func TestRunSelfReview_ExtractsPatterns(t *testing.T) {
	backend := &mockSelfReviewBackend{
		output: "REVIEW_FIXED: found unwired config field\nSUSPICIOUS_VALUE: hardcoded 999",
	}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	extractor := &mockSelfReviewExtractor{
		extractFunc: func(_ context.Context, output string, projectPath string) (*memory.ExtractionResult, error) {
			// Delegate to real PatternExtractor logic via simple simulation
			result := &memory.ExtractionResult{
				ExecutionID: "test",
				ProjectPath: projectPath,
			}
			if strings.Contains(output, "REVIEW_FIXED") {
				result.AntiPatterns = append(result.AntiPatterns, &memory.ExtractedPattern{
					Type:        "review_fix",
					Title:       "Self-review fix",
					Description: "Self-review found and fixed issues",
					Confidence:  0.5,
				})
			}
			if strings.Contains(output, "SUSPICIOUS_VALUE") {
				result.AntiPatterns = append(result.AntiPatterns, &memory.ExtractedPattern{
					Type:        "suspicious_value",
					Title:       "Suspicious hardcoded value",
					Description: "Hardcoded constant needs verification",
					Confidence:  0.5,
				})
			}
			return result, nil
		},
	}
	runner.SetSelfReviewExtractor(extractor)

	task := &Task{
		ID:          "SR-001",
		Title:       "Add user authentication with JWT tokens and session management",
		Description: "Implement full auth flow with JWT tokens, refresh tokens, and session management",
		ProjectPath: t.TempDir(),
	}
	state := &progressState{}

	err := runner.runSelfReview(context.Background(), task, state)
	if err != nil {
		t.Fatalf("runSelfReview returned error: %v", err)
	}

	extractor.mu.Lock()
	defer extractor.mu.Unlock()

	if extractor.extractCalls != 1 {
		t.Errorf("expected 1 extract call, got %d", extractor.extractCalls)
	}
	if extractor.saveCalls != 1 {
		t.Errorf("expected 1 save call, got %d", extractor.saveCalls)
	}
	if extractor.lastResult == nil {
		t.Fatal("expected lastResult to be set")
	}
	if len(extractor.lastResult.AntiPatterns) != 2 {
		t.Errorf("expected 2 anti-patterns, got %d", len(extractor.lastResult.AntiPatterns))
	}
}

func TestRunSelfReview_SkipsExtractionWhenNoExtractor(t *testing.T) {
	backend := &mockSelfReviewBackend{
		output: "REVIEW_FIXED: found issue",
	}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	// No extractor set — should not panic
	task := &Task{
		ID:          "SR-002",
		Title:       "Add complex feature with multiple components",
		Description: "Implement a complex multi-step feature requiring significant changes",
		ProjectPath: t.TempDir(),
	}
	state := &progressState{}

	err := runner.runSelfReview(context.Background(), task, state)
	if err != nil {
		t.Fatalf("runSelfReview returned error: %v", err)
	}
}

func TestRunSelfReview_SkipsExtractionWhenEmptyOutput(t *testing.T) {
	backend := &mockSelfReviewBackend{
		output: "", // empty output
	}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	extractor := &mockSelfReviewExtractor{}
	runner.SetSelfReviewExtractor(extractor)

	task := &Task{
		ID:          "SR-003",
		Title:       "Add complex feature with multiple components",
		Description: "Implement a complex multi-step feature requiring significant changes",
		ProjectPath: t.TempDir(),
	}
	state := &progressState{}

	err := runner.runSelfReview(context.Background(), task, state)
	if err != nil {
		t.Fatalf("runSelfReview returned error: %v", err)
	}

	extractor.mu.Lock()
	defer extractor.mu.Unlock()

	if extractor.extractCalls != 0 {
		t.Errorf("expected 0 extract calls for empty output, got %d", extractor.extractCalls)
	}
}

func TestRunSelfReview_SkipsSaveWhenNoPatternsExtracted(t *testing.T) {
	backend := &mockSelfReviewBackend{
		output: "REVIEW_PASSED",
	}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	extractor := &mockSelfReviewExtractor{
		extractFunc: func(_ context.Context, _ string, _ string) (*memory.ExtractionResult, error) {
			// No patterns found
			return &memory.ExtractionResult{
				Patterns:     make([]*memory.ExtractedPattern, 0),
				AntiPatterns: make([]*memory.ExtractedPattern, 0),
			}, nil
		},
	}
	runner.SetSelfReviewExtractor(extractor)

	task := &Task{
		ID:          "SR-004",
		Title:       "Add complex feature with multiple components",
		Description: "Implement a complex multi-step feature requiring significant changes",
		ProjectPath: t.TempDir(),
	}
	state := &progressState{}

	err := runner.runSelfReview(context.Background(), task, state)
	if err != nil {
		t.Fatalf("runSelfReview returned error: %v", err)
	}

	extractor.mu.Lock()
	defer extractor.mu.Unlock()

	if extractor.extractCalls != 1 {
		t.Errorf("expected 1 extract call, got %d", extractor.extractCalls)
	}
	if extractor.saveCalls != 0 {
		t.Errorf("expected 0 save calls when no patterns extracted, got %d", extractor.saveCalls)
	}
}

func TestLocalModeSkipsNavigatorAutoInit(t *testing.T) {
	projectDir := t.TempDir()

	backend := &mockSelfReviewBackend{output: "done"}
	runner := NewRunnerWithBackend(backend)
	runner.config = &BackendConfig{
		Navigator: &NavigatorConfig{
			AutoInit: true,
		},
	}
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true

	task := &Task{
		ID:          "LOCAL-001",
		Title:       "Local mode task",
		Description: "Should skip Navigator init",
		ProjectPath: projectDir,
		LocalMode:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() not successful: %s", result.Error)
	}

	// Verify .agent/ directory was NOT created
	agentDir := filepath.Join(projectDir, ".agent")
	if _, err := os.Stat(agentDir); err == nil {
		t.Errorf(".agent/ directory was created in LocalMode — Navigator auto-init should be skipped")
	}
}

func TestLocalModeRunsQualityGates(t *testing.T) {
	// Quality gates are enabled in LocalMode because sandbox dependencies are pre-installed.
	projectDir := t.TempDir()

	backend := &mockSelfReviewBackend{output: "done"}
	runner := NewRunnerWithBackend(backend)
	runner.config = &BackendConfig{}
	runner.SetRecordingEnabled(false)
	runner.skipPreflightChecks = true

	qualityGateCalled := false
	runner.SetQualityCheckerFactory(func(taskID, projectPath string) QualityChecker {
		qualityGateCalled = true
		return &mockQualityChecker{
			outcome: &QualityOutcome{Passed: true},
		}
	})

	task := &Task{
		ID:          "LOCAL-QG-001",
		Title:       "Local mode quality gate test",
		Description: "Quality gates should run in LocalMode",
		ProjectPath: projectDir,
		LocalMode:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute() not successful: %s", result.Error)
	}

	if !qualityGateCalled {
		t.Errorf("quality checker factory was NOT called in LocalMode — quality gates should run")
	}
}

// pathCapturingReviewBackend records the ProjectPath passed to Execute so the
// self-review worktree-isolation test can assert which tree review runs in.
type pathCapturingReviewBackend struct{ gotPath string }

func (b *pathCapturingReviewBackend) Name() string      { return "capture" }
func (b *pathCapturingReviewBackend) IsAvailable() bool { return true }
func (b *pathCapturingReviewBackend) Execute(_ context.Context, opts ExecuteOptions) (*BackendResult, error) {
	b.gotPath = opts.ProjectPath
	return &BackendResult{Success: true, Output: "REVIEW_PASSED"}, nil
}

// TestRunSelfReview_RunsInWorktreeNotProjectRoot guards GH-936 worktree
// isolation: self-review must run in state.executionPath (the worktree), not
// task.ProjectPath (the shared project root).
func TestRunSelfReview_RunsInWorktreeNotProjectRoot(t *testing.T) {
	backend := &pathCapturingReviewBackend{}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	worktree := t.TempDir()
	task := &Task{
		ID:          "SR-WT-1",
		Title:       "Add complex multi-component feature spanning several files",
		Description: "Implement a non-trivial feature so self-review is not skipped as trivial",
		ProjectPath: t.TempDir(), // shared project root — must NOT be used for review
	}
	state := &progressState{executionPath: worktree}

	if err := runner.runSelfReview(context.Background(), task, state); err != nil {
		t.Fatalf("runSelfReview: %v", err)
	}
	if backend.gotPath != worktree {
		t.Errorf("self-review ProjectPath = %q, want worktree %q (not project root %q)",
			backend.gotPath, worktree, task.ProjectPath)
	}
}

// TestRunSelfReview_FallsBackToProjectPath confirms the fallback when no
// worktree path is set (unit-test / no-worktree mode preserves old behavior).
func TestRunSelfReview_FallsBackToProjectPath(t *testing.T) {
	backend := &pathCapturingReviewBackend{}
	runner := NewRunnerWithBackend(backend)
	runner.skipPreflightChecks = true

	proj := t.TempDir()
	task := &Task{
		ID:          "SR-WT-2",
		Title:       "Add complex multi-component feature spanning several files",
		Description: "Implement a non-trivial feature so self-review is not skipped as trivial",
		ProjectPath: proj,
	}
	state := &progressState{} // no executionPath

	if err := runner.runSelfReview(context.Background(), task, state); err != nil {
		t.Fatalf("runSelfReview: %v", err)
	}
	if backend.gotPath != proj {
		t.Errorf("fallback ProjectPath = %q, want %q", backend.gotPath, proj)
	}
}

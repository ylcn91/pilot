package executor

import (
	"os"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// newTestOutcomeTracker creates a tracker backed by a temporary SQLite store.
func newTestOutcomeTracker(t *testing.T) (*memory.ModelOutcomeTracker, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "routing-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := memory.NewStore(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create store: %v", err)
	}
	tracker := memory.NewModelOutcomeTracker(store)
	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return tracker, cleanup
}

func TestModelRouter_SelectModelEscalation(t *testing.T) {
	tracker, cleanup := newTestOutcomeTracker(t)
	defer cleanup()

	config := &ModelRoutingConfig{
		Enabled: true,
		Trivial: "haiku",
		Simple:  "sonnet",
		Medium:  "sonnet",
		Complex: "opus",
	}
	router := NewModelRouter(config, nil)
	router.SetOutcomeTracker(tracker)

	// Record enough failures to exceed 30% threshold (4 failures out of 10)
	for i := 0; i < 4; i++ {
		if err := tracker.RecordOutcome("trivial", "haiku", "failure", 1000, time.Minute); err != nil {
			t.Fatalf("failed to record outcome: %v", err)
		}
	}
	for i := 0; i < 6; i++ {
		if err := tracker.RecordOutcome("trivial", "haiku", "success", 1000, time.Minute); err != nil {
			t.Fatalf("failed to record outcome: %v", err)
		}
	}

	// Trivial task should be escalated from haiku to sonnet
	task := &Task{Description: "Fix typo in README"}
	got := router.SelectModel(task)
	if got != "sonnet" {
		t.Errorf("Expected escalation to 'sonnet', got %q", got)
	}
}

func TestModelRouter_SelectModelNoEscalationBelowThreshold(t *testing.T) {
	tracker, cleanup := newTestOutcomeTracker(t)
	defer cleanup()

	config := &ModelRoutingConfig{
		Enabled: true,
		Trivial: "haiku",
		Simple:  "sonnet",
		Medium:  "sonnet",
		Complex: "opus",
	}
	router := NewModelRouter(config, nil)
	router.SetOutcomeTracker(tracker)

	// Record outcomes below threshold (2 failures out of 10 = 20%)
	for i := 0; i < 2; i++ {
		if err := tracker.RecordOutcome("trivial", "haiku", "failure", 1000, time.Minute); err != nil {
			t.Fatalf("failed to record outcome: %v", err)
		}
	}
	for i := 0; i < 8; i++ {
		if err := tracker.RecordOutcome("trivial", "haiku", "success", 1000, time.Minute); err != nil {
			t.Fatalf("failed to record outcome: %v", err)
		}
	}

	// Should NOT escalate — failure rate 20% < 30% threshold
	task := &Task{Description: "Fix typo in README"}
	got := router.SelectModel(task)
	if got != "haiku" {
		t.Errorf("Expected no escalation (haiku), got %q", got)
	}
}

func TestModelRouter_SelectModelNilTrackerNoEscalation(t *testing.T) {
	config := &ModelRoutingConfig{
		Enabled: true,
		Trivial: "haiku",
		Simple:  "sonnet",
		Medium:  "sonnet",
		Complex: "opus",
	}
	router := NewModelRouter(config, nil)
	// No tracker set — should behave exactly as before

	task := &Task{Description: "Fix typo in README"}
	got := router.SelectModel(task)
	if got != "haiku" {
		t.Errorf("Expected 'haiku' without tracker, got %q", got)
	}
}

func TestResolveComplexity_LLMUpgradesHeuristic(t *testing.T) {
	router := NewModelRouter(nil, nil)
	// Attach LLM classifier returning "high"
	classifier := newEffortClassifierWithRunner(mockEffortRunner("high", "large refactor"))
	router.SetEffortClassifier(classifier)

	// "remove unused" matches trivial heuristic, but LLM says high → complex
	task := &Task{ID: "GH-2131", Description: "remove unused imports"}
	got := router.resolveComplexity(task)
	if got != ComplexityComplex {
		t.Errorf("Expected ComplexityComplex (LLM floor), got %s", got)
	}
}

func TestResolveComplexity_HeuristicWinsWhenHigher(t *testing.T) {
	router := NewModelRouter(nil, nil)
	// Attach LLM classifier returning "low"
	classifier := newEffortClassifierWithRunner(mockEffortRunner("low", "trivial"))
	router.SetEffortClassifier(classifier)

	// Heuristic returns complex, LLM says low → heuristic wins
	task := &Task{ID: "GH-999", Description: "Refactor the authentication system"}
	got := router.resolveComplexity(task)
	if got != ComplexityComplex {
		t.Errorf("Expected ComplexityComplex (heuristic wins), got %s", got)
	}
}

func TestResolveComplexity_NoClassifier(t *testing.T) {
	router := NewModelRouter(nil, nil)
	// No LLM classifier — pure heuristic

	task := &Task{ID: "GH-1", Description: "Fix typo in README"}
	got := router.resolveComplexity(task)
	heuristic := DetectComplexity(task)
	if got != heuristic {
		t.Errorf("Expected heuristic %s, got %s", heuristic, got)
	}
}

func TestSelectModel_UsesResolvedComplexity(t *testing.T) {
	config := &ModelRoutingConfig{
		Enabled: true,
		Trivial: "haiku",
		Simple:  "sonnet",
		Medium:  "sonnet",
		Complex: "opus",
	}
	router := NewModelRouter(config, nil)
	// LLM says "high" → should select opus despite trivial heuristic
	classifier := newEffortClassifierWithRunner(mockEffortRunner("high", "security sensitive"))
	router.SetEffortClassifier(classifier)

	task := &Task{ID: "GH-2145", Description: "remove unused imports"}
	got := router.SelectModel(task)
	if got != "opus" {
		t.Errorf("Expected 'opus' (LLM effort floor), got %q", got)
	}
}

func TestSelectTimeout_UsesResolvedComplexity(t *testing.T) {
	timeoutConfig := &TimeoutConfig{
		Default: "30m",
		Trivial: "5m",
		Simple:  "10m",
		Medium:  "30m",
		Complex: "60m",
	}
	router := NewModelRouter(nil, timeoutConfig)
	// LLM says "high" → should get 60m despite trivial heuristic
	classifier := newEffortClassifierWithRunner(mockEffortRunner("high", "large refactor"))
	router.SetEffortClassifier(classifier)

	task := &Task{ID: "GH-2145", Description: "remove unused imports"}
	got := router.SelectTimeout(task)
	if got != 60*time.Minute {
		t.Errorf("Expected 60m (LLM effort floor), got %v", got)
	}
}

func TestModelRouter_SetOutcomeTracker(t *testing.T) {
	tracker, cleanup := newTestOutcomeTracker(t)
	defer cleanup()

	router := NewModelRouter(nil, nil)
	if router.outcomeTracker != nil {
		t.Error("Expected nil outcome tracker by default")
	}

	router.SetOutcomeTracker(tracker)
	if router.outcomeTracker == nil {
		t.Error("Expected outcome tracker to be set")
	}
}

// TestDefaultModelRoutingConfig_EnabledByDefault verifies that model routing is active
// by default, as required by GH-2807.
func TestDefaultModelRoutingConfig_EnabledByDefault(t *testing.T) {
	cfg := DefaultModelRoutingConfig()
	if !cfg.Enabled {
		t.Error("DefaultModelRoutingConfig().Enabled = false, want true")
	}
}

// TestDefaultModelRoutingConfig_UserOverrideWins verifies that an explicit enabled:false
// in user config is respected, overriding the new default.
func TestDefaultModelRoutingConfig_UserOverrideWins(t *testing.T) {
	router := NewModelRouterWithEffort(
		&ModelRoutingConfig{Enabled: false, Trivial: "haiku", Simple: "sonnet"},
		nil,
		nil,
	)
	task := &Task{Description: "Fix typo in README"}
	model := router.SelectModel(task)
	if model != "" {
		t.Errorf("user config enabled:false should disable routing; SelectModel() = %q, want empty", model)
	}
}

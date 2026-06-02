package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSaveExtractedPatterns_CIRecurrenceBoost(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	// Simulate first CI failure → extract CI patterns
	ciLogs := "undefined: processItem"
	ciPatterns := extractor.extractCIErrorPatterns(ciLogs)
	if len(ciPatterns) == 0 {
		t.Fatal("expected at least 1 CI error pattern")
	}

	result1 := &ExtractionResult{
		ExecutionID:  "ci-run-1",
		ProjectPath:  "/test/project",
		AntiPatterns: ciPatterns,
		Patterns:     make([]*ExtractedPattern, 0),
	}

	if err := extractor.SaveExtractedPatterns(ctx, result1); err != nil {
		t.Fatalf("first SaveExtractedPatterns failed: %v", err)
	}

	// Record initial confidence
	allPatterns := patternStore.GetByType(ciPatterns[0].Type)
	if len(allPatterns) == 0 {
		t.Fatal("expected pattern to be saved")
	}
	initialConfidence := allPatterns[0].Confidence

	// Simulate same CI failure recurring → extract again
	ciPatterns2 := extractor.extractCIErrorPatterns(ciLogs)
	result2 := &ExtractionResult{
		ExecutionID:  "ci-run-2",
		ProjectPath:  "/test/project",
		AntiPatterns: ciPatterns2,
		Patterns:     make([]*ExtractedPattern, 0),
	}

	if err := extractor.SaveExtractedPatterns(ctx, result2); err != nil {
		t.Fatalf("second SaveExtractedPatterns failed: %v", err)
	}

	// Verify confidence was boosted by 1.5x
	boostedPatterns := patternStore.GetByType(ciPatterns[0].Type)
	var found *GlobalPattern
	for _, p := range boostedPatterns {
		if strings.Contains(p.Title, ciPatterns[0].Title) {
			found = p
			break
		}
	}

	if found == nil {
		t.Fatal("boosted pattern not found")
	}

	expectedConfidence := min(0.95, initialConfidence*1.5)
	if found.Confidence != expectedConfidence {
		t.Errorf("confidence = %f, want %f (initial %f * 1.5)", found.Confidence, expectedConfidence, initialConfidence)
	}

	// Verify no duplicate was created
	count := 0
	for _, p := range boostedPatterns {
		if strings.Contains(p.Title, ciPatterns[0].Title) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 pattern (deduped), got %d", count)
	}
}

func TestSaveExtractedPatterns_CIRecurrenceBoost_CappedAt095(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	ciLogs := "undefined: processItem"

	// Save repeatedly to push confidence toward cap
	for i := 0; i < 10; i++ {
		ciPatterns := extractor.extractCIErrorPatterns(ciLogs)
		result := &ExtractionResult{
			ExecutionID:  fmt.Sprintf("ci-run-%d", i),
			ProjectPath:  "/test/project",
			AntiPatterns: ciPatterns,
			Patterns:     make([]*ExtractedPattern, 0),
		}
		if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
			t.Fatalf("SaveExtractedPatterns iteration %d failed: %v", i, err)
		}
	}

	// Verify confidence is capped at 0.95
	allPatterns := patternStore.GetByType(PatternTypeError)
	for _, p := range allPatterns {
		if p.Confidence > 0.95 {
			t.Errorf("confidence %f exceeds cap of 0.95 for pattern %q", p.Confidence, p.Title)
		}
	}
}

// TestCIPatternRecurrence_EndToEnd is an integration test that validates the full loop:
// CI failure → pattern extracted → same failure recurs → confidence boosted →
// pattern appears in PatternContext.InjectPatterns() output.
func TestCIPatternRecurrence_EndToEnd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "extractor-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	patternStore, _ := NewGlobalPatternStore(tmpDir)
	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	projectPath := "/test/ci-project"
	ciLogs := "--- FAIL: TestProcess (0.05s)\n    process_test.go:42: expected nil, got error"

	// Step 1: First CI failure → extract patterns
	ciPatterns := extractor.extractCIErrorPatterns(ciLogs)
	if len(ciPatterns) == 0 {
		t.Fatal("expected CI error patterns from FAIL log line")
	}
	result1 := &ExtractionResult{
		ExecutionID:  "ci-run-1",
		ProjectPath:  projectPath,
		AntiPatterns: ciPatterns,
		Patterns:     make([]*ExtractedPattern, 0),
	}
	if err := extractor.SaveExtractedPatterns(ctx, result1); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Verify CrossPattern was created in SQLite
	crossPatterns, err := store.GetCrossPatternsForProject(projectPath, true)
	if err != nil {
		t.Fatalf("GetCrossPatternsForProject failed: %v", err)
	}
	if len(crossPatterns) == 0 {
		t.Fatal("expected CrossPattern to be saved to SQLite for CI pattern")
	}
	initialCrossConfidence := crossPatterns[0].Confidence

	// Step 2: Same CI failure recurs → extract and save again
	ciPatterns2 := extractor.extractCIErrorPatterns(ciLogs)
	result2 := &ExtractionResult{
		ExecutionID:  "ci-run-2",
		ProjectPath:  projectPath,
		AntiPatterns: ciPatterns2,
		Patterns:     make([]*ExtractedPattern, 0),
	}
	if err := extractor.SaveExtractedPatterns(ctx, result2); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	// Step 3: Verify confidence was boosted in CrossPattern
	crossPatternsAfter, err := store.GetCrossPatternsForProject(projectPath, true)
	if err != nil {
		t.Fatalf("GetCrossPatternsForProject after boost failed: %v", err)
	}
	if len(crossPatternsAfter) == 0 {
		t.Fatal("expected CrossPattern after boost")
	}

	// Find the matching pattern
	var boostedCross *CrossPattern
	for _, cp := range crossPatternsAfter {
		if cp.IsAntiPattern {
			boostedCross = cp
			break
		}
	}
	if boostedCross == nil {
		t.Fatal("no anti-pattern CrossPattern found after boost")
	}

	expectedBoostedConfidence := min(0.95, initialCrossConfidence*1.5)
	if boostedCross.Confidence != expectedBoostedConfidence {
		t.Errorf("CrossPattern confidence = %f, want %f (boosted from %f)",
			boostedCross.Confidence, expectedBoostedConfidence, initialCrossConfidence)
	}

	// Step 4: Verify pattern appears in FormatForPrompt output
	queryService := NewPatternQueryService(store)
	promptBlock, err := queryService.FormatForPrompt(ctx, projectPath, "fix", "CI test failure fix")
	if err != nil {
		t.Fatalf("FormatForPrompt failed: %v", err)
	}

	if promptBlock == "" {
		t.Fatal("FormatForPrompt returned empty string; expected CI pattern to be injected")
	}

	if !strings.Contains(promptBlock, "Anti-Patterns to Avoid") {
		t.Error("expected 'Anti-Patterns to Avoid' section in prompt block")
	}

	// Verify the specific CI pattern title appears
	if !strings.Contains(promptBlock, "Test failure") {
		t.Errorf("expected 'Test failure' pattern in prompt block, got:\n%s", promptBlock)
	}
}

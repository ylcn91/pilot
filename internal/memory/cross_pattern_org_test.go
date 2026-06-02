package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOrgPatternStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-org-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	orgStore, err := NewOrgPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create org store: %v", err)
	}

	// Create aggregated pattern
	pattern := &AggregatedPattern{
		ID:           "org_1",
		Type:         "code",
		Title:        "Org Pattern",
		Description:  "A pattern aggregated across projects",
		Confidence:   0.85,
		Occurrences:  15,
		ProjectCount: 3,
		Projects: []ProjectMention{
			{ProjectPath: "/project/a", Uses: 5, SuccessRate: 0.8},
			{ProjectPath: "/project/b", Uses: 7, SuccessRate: 0.9},
			{ProjectPath: "/project/c", Uses: 3, SuccessRate: 0.75},
		},
		CreatedAt: time.Now(),
	}

	if err := orgStore.Update(pattern); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Retrieve
	retrieved, ok := orgStore.Get("org_1")
	if !ok {
		t.Fatal("pattern not found")
	}
	if retrieved.ProjectCount != 3 {
		t.Errorf("expected 3 projects, got %d", retrieved.ProjectCount)
	}

	// Test persistence
	orgStore2, err := NewOrgPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reload org store: %v", err)
	}

	reloaded, ok := orgStore2.Get("org_1")
	if !ok {
		t.Fatal("pattern not found after reload")
	}
	if reloaded.Title != "Org Pattern" {
		t.Errorf("expected title 'Org Pattern', got '%s'", reloaded.Title)
	}
}

func TestPatternSync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-test-sync-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	sync, err := NewPatternSync(store, tmpDir)
	if err != nil {
		t.Fatalf("failed to create sync: %v", err)
	}

	ctx := context.Background()

	// Create pattern and links
	pattern := &CrossPattern{
		ID:         "sync_test_1",
		Type:       "code",
		Title:      "Sync test pattern",
		Confidence: 0.7,
		Scope:      "org",
	}
	if err := store.SaveCrossPattern(pattern); err != nil {
		t.Fatalf("SaveCrossPattern failed: %v", err)
	}

	_ = store.LinkPatternToProject("sync_test_1", "/project/a")
	_ = store.LinkPatternToProject("sync_test_1", "/project/b")

	// Sync from project
	if err := sync.SyncFromProject(ctx, "/project/a"); err != nil {
		t.Fatalf("SyncFromProject failed: %v", err)
	}

	// Check aggregated patterns
	orgPatterns := sync.GetOrgPatterns()
	if len(orgPatterns) != 1 {
		t.Errorf("expected 1 org pattern, got %d", len(orgPatterns))
	}

	// Test export
	exportPath := filepath.Join(tmpDir, "export.json")
	if err := sync.ExportPatterns(ctx, exportPath, 0.5); err != nil {
		t.Fatalf("ExportPatterns failed: %v", err)
	}

	// Verify export file exists
	if _, err := os.Stat(exportPath); os.IsNotExist(err) {
		t.Error("export file not created")
	}
}

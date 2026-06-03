package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteFileAtomic_FileIntactAfterSave verifies the crash-safe save path
// leaves a complete, well-formed file and no leftover temp files.
func TestWriteFileAtomic_FileIntactAfterSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "atomic-write-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	path := filepath.Join(tmpDir, "data.json")
	want := []byte(`{"key":"value"}`)
	if err := writeFileAtomic(path, want); err != nil {
		t.Fatalf("writeFileAtomic failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("file contents = %q, want %q", got, want)
	}

	// Overwriting an existing file must also yield exact contents (no append,
	// no truncation residue) and preserve 0644 mode.
	want2 := []byte(`{"key":"updated","extra":1}`)
	if err := writeFileAtomic(path, want2); err != nil {
		t.Fatalf("writeFileAtomic overwrite failed: %v", err)
	}
	got2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back after overwrite failed: %v", err)
	}
	if string(got2) != string(want2) {
		t.Errorf("overwritten contents = %q, want %q", got2, want2)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("file mode = %o, want 0644", info.Mode().Perm())
	}

	assertNoLeftoverTmp(t, tmpDir)
}

// TestOrgPatternStore_SaveIntact verifies OrgPatternStore.save (via Update)
// produces a valid, parseable org_patterns.json and leaves no temp files.
func TestOrgPatternStore_SaveIntact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "org-save-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewOrgPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewOrgPatternStore failed: %v", err)
	}

	if err := store.Update(&AggregatedPattern{ID: "p1", Title: "One", Confidence: 0.9}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "org_patterns.json"))
	if err != nil {
		t.Fatalf("read org_patterns.json failed: %v", err)
	}
	var patterns []*AggregatedPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		t.Fatalf("org_patterns.json is not intact valid JSON: %v", err)
	}
	if len(patterns) != 1 || patterns[0].ID != "p1" {
		t.Errorf("unexpected persisted patterns: %+v", patterns)
	}

	assertNoLeftoverTmp(t, tmpDir)
}

// TestGlobalPatternStore_SaveIntact verifies GlobalPatternStore.saveUnlocked
// (via Add) produces a valid, parseable global_patterns.json with no temp files.
func TestGlobalPatternStore_SaveIntact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "global-save-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore failed: %v", err)
	}

	if err := store.Add(&GlobalPattern{ID: "g1", Type: PatternTypeCode, Title: "Code"}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "global_patterns.json"))
	if err != nil {
		t.Fatalf("read global_patterns.json failed: %v", err)
	}
	var patterns []*GlobalPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		t.Fatalf("global_patterns.json is not intact valid JSON: %v", err)
	}
	if len(patterns) != 1 || patterns[0].ID != "g1" {
		t.Errorf("unexpected persisted patterns: %+v", patterns)
	}

	assertNoLeftoverTmp(t, tmpDir)
}

func assertNoLeftoverTmp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewProfileManager(t *testing.T) {
	pm := NewProfileManager("/global/path", "/project/path")

	if pm.globalPath != "/global/path" {
		t.Errorf("globalPath = %q, want %q", pm.globalPath, "/global/path")
	}

	if pm.projectPath != "/project/path" {
		t.Errorf("projectPath = %q, want %q", pm.projectPath, "/project/path")
	}
}

func TestProfileManager_LoadEmpty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pm := NewProfileManager(
		filepath.Join(tmpDir, "global.json"),
		filepath.Join(tmpDir, "project.json"),
	)

	profile, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if profile == nil {
		t.Fatal("Load() returned nil profile")
	}

	if profile.Conventions == nil {
		t.Error("Load() returned profile with nil Conventions map")
	}

	if profile.Verbosity != "" {
		t.Errorf("Verbosity = %q, want empty", profile.Verbosity)
	}
}

func TestProfileManager_SaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pm := NewProfileManager(
		filepath.Join(tmpDir, "global.json"),
		filepath.Join(tmpDir, "project.json"),
	)

	profile := &UserProfile{
		Verbosity:    "concise",
		Frameworks:   []string{"gin", "gorm"},
		Conventions:  map[string]string{"indent": "tabs", "naming": "camelCase"},
		CodePatterns: []string{"early_returns", "guard_clauses"},
		Corrections: []Correction{
			{Pattern: "use fmt.Printf", Correction: "use slog instead", Count: 3},
		},
	}

	// Save to global
	if err := pm.Save(profile, true); err != nil {
		t.Fatalf("Save(global) error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(tmpDir, "global.json")); os.IsNotExist(err) {
		t.Error("global.json was not created")
	}

	// Load and verify
	loaded, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Verbosity != "concise" {
		t.Errorf("Verbosity = %q, want %q", loaded.Verbosity, "concise")
	}

	if len(loaded.Frameworks) != 2 {
		t.Errorf("len(Frameworks) = %d, want 2", len(loaded.Frameworks))
	}

	if loaded.Conventions["indent"] != "tabs" {
		t.Errorf("Conventions[indent] = %q, want %q", loaded.Conventions["indent"], "tabs")
	}

	if len(loaded.Corrections) != 1 {
		t.Errorf("len(Corrections) = %d, want 1", len(loaded.Corrections))
	}

	if loaded.Corrections[0].Count != 3 {
		t.Errorf("Corrections[0].Count = %d, want 3", loaded.Corrections[0].Count)
	}
}

func TestProfileManager_MergeProfiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	globalPath := filepath.Join(tmpDir, "global.json")
	projectPath := filepath.Join(tmpDir, "project.json")
	pm := NewProfileManager(globalPath, projectPath)

	// Create global profile
	globalProfile := &UserProfile{
		Verbosity:    "detailed",
		Frameworks:   []string{"gin"},
		Conventions:  map[string]string{"indent": "spaces"},
		CodePatterns: []string{"early_returns"},
	}
	if err := pm.Save(globalProfile, true); err != nil {
		t.Fatalf("Save(global) error = %v", err)
	}

	// Create project profile (overrides)
	projectProfile := &UserProfile{
		Verbosity:    "concise",
		Frameworks:   []string{"echo"}, // Different framework
		Conventions:  map[string]string{"indent": "tabs", "test": "value"},
		CodePatterns: []string{"guard_clauses"},
	}
	if err := pm.Save(projectProfile, false); err != nil {
		t.Fatalf("Save(project) error = %v", err)
	}

	// Load merged profile
	merged, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verbosity should be overridden by project
	if merged.Verbosity != "concise" {
		t.Errorf("Verbosity = %q, want %q (project override)", merged.Verbosity, "concise")
	}

	// Frameworks should be merged (deduplicated)
	if len(merged.Frameworks) != 2 {
		t.Errorf("len(Frameworks) = %d, want 2 (merged)", len(merged.Frameworks))
	}

	// Conventions should be merged with project taking precedence
	if merged.Conventions["indent"] != "tabs" {
		t.Errorf("Conventions[indent] = %q, want %q (project override)", merged.Conventions["indent"], "tabs")
	}
	if merged.Conventions["test"] != "value" {
		t.Errorf("Conventions[test] = %q, want %q", merged.Conventions["test"], "value")
	}

	// CodePatterns should be merged
	if len(merged.CodePatterns) != 2 {
		t.Errorf("len(CodePatterns) = %d, want 2 (merged)", len(merged.CodePatterns))
	}
}

func TestProfileManager_MergeCorrectionsPrecedence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	globalPath := filepath.Join(tmpDir, "global.json")
	projectPath := filepath.Join(tmpDir, "project.json")
	pm := NewProfileManager(globalPath, projectPath)

	// Create global profile with correction
	globalProfile := &UserProfile{
		Conventions: make(map[string]string),
		Corrections: []Correction{
			{Pattern: "shared", Correction: "global correction", Count: 5},
			{Pattern: "global only", Correction: "only in global", Count: 1},
		},
	}
	if err := pm.Save(globalProfile, true); err != nil {
		t.Fatalf("Save(global) error = %v", err)
	}

	// Create project profile with override
	projectProfile := &UserProfile{
		Conventions: make(map[string]string),
		Corrections: []Correction{
			{Pattern: "shared", Correction: "project correction", Count: 10},
			{Pattern: "project only", Correction: "only in project", Count: 2},
		},
	}
	if err := pm.Save(projectProfile, false); err != nil {
		t.Fatalf("Save(project) error = %v", err)
	}

	// Load merged
	merged, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should have 3 corrections
	if len(merged.Corrections) != 3 {
		t.Errorf("len(Corrections) = %d, want 3", len(merged.Corrections))
	}

	// Verify project takes precedence for shared pattern
	for _, c := range merged.Corrections {
		if c.Pattern == "shared" {
			if c.Correction != "project correction" {
				t.Errorf("shared correction = %q, want project override", c.Correction)
			}
			if c.Count != 10 {
				t.Errorf("shared count = %d, want 10", c.Count)
			}
		}
	}
}

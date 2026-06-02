package memory

import (
	"testing"
)

func TestUserProfile_RecordCorrection(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// First correction
	profile.RecordCorrection("use println", "use slog.Info")

	if len(profile.Corrections) != 1 {
		t.Fatalf("len(Corrections) = %d, want 1", len(profile.Corrections))
	}

	if profile.Corrections[0].Count != 1 {
		t.Errorf("Count = %d, want 1", profile.Corrections[0].Count)
	}

	// Same pattern again - should increment count
	profile.RecordCorrection("use println", "use structured logging")

	if len(profile.Corrections) != 1 {
		t.Errorf("len(Corrections) = %d, want 1 (same pattern)", len(profile.Corrections))
	}

	if profile.Corrections[0].Count != 2 {
		t.Errorf("Count = %d, want 2", profile.Corrections[0].Count)
	}

	if profile.Corrections[0].Correction != "use structured logging" {
		t.Errorf("Correction = %q, want updated value", profile.Corrections[0].Correction)
	}

	// Different pattern - should add new correction
	profile.RecordCorrection("use fmt.Errorf", "use errors.New")

	if len(profile.Corrections) != 2 {
		t.Errorf("len(Corrections) = %d, want 2", len(profile.Corrections))
	}
}

func TestUserProfile_SetGetPreference(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// Get non-existent preference
	if got := profile.GetPreference("nonexistent"); got != "" {
		t.Errorf("GetPreference(nonexistent) = %q, want empty", got)
	}

	// Set and get preference
	profile.SetPreference("indent", "tabs")

	if got := profile.GetPreference("indent"); got != "tabs" {
		t.Errorf("GetPreference(indent) = %q, want %q", got, "tabs")
	}

	// Override preference
	profile.SetPreference("indent", "spaces")

	if got := profile.GetPreference("indent"); got != "spaces" {
		t.Errorf("GetPreference(indent) after override = %q, want %q", got, "spaces")
	}
}

func TestUserProfile_SetPreferenceNilMap(t *testing.T) {
	profile := &UserProfile{}

	// Should handle nil Conventions map
	profile.SetPreference("key", "value")

	if profile.Conventions == nil {
		t.Error("SetPreference should initialize Conventions map")
	}

	if got := profile.GetPreference("key"); got != "value" {
		t.Errorf("GetPreference(key) = %q, want %q", got, "value")
	}
}

func TestUserProfile_GetPreferenceNilMap(t *testing.T) {
	profile := &UserProfile{}

	// Should handle nil Conventions map gracefully
	if got := profile.GetPreference("key"); got != "" {
		t.Errorf("GetPreference with nil map = %q, want empty", got)
	}
}

func TestUserProfile_AddFramework(t *testing.T) {
	profile := &UserProfile{}

	profile.AddFramework("gin")
	if len(profile.Frameworks) != 1 {
		t.Errorf("len(Frameworks) = %d, want 1", len(profile.Frameworks))
	}

	// Add same framework again - should not duplicate
	profile.AddFramework("gin")
	if len(profile.Frameworks) != 1 {
		t.Errorf("len(Frameworks) = %d, want 1 (no duplicate)", len(profile.Frameworks))
	}

	// Add different framework
	profile.AddFramework("gorm")
	if len(profile.Frameworks) != 2 {
		t.Errorf("len(Frameworks) = %d, want 2", len(profile.Frameworks))
	}
}

func TestUserProfile_AddCodePattern(t *testing.T) {
	profile := &UserProfile{}

	profile.AddCodePattern("early_returns")
	if len(profile.CodePatterns) != 1 {
		t.Errorf("len(CodePatterns) = %d, want 1", len(profile.CodePatterns))
	}

	// Add same pattern again - should not duplicate
	profile.AddCodePattern("early_returns")
	if len(profile.CodePatterns) != 1 {
		t.Errorf("len(CodePatterns) = %d, want 1 (no duplicate)", len(profile.CodePatterns))
	}

	// Add different pattern
	profile.AddCodePattern("guard_clauses")
	if len(profile.CodePatterns) != 2 {
		t.Errorf("len(CodePatterns) = %d, want 2", len(profile.CodePatterns))
	}
}

func TestUserProfile_GetCorrection(t *testing.T) {
	profile := &UserProfile{
		Corrections: []Correction{
			{Pattern: "use println", Correction: "use slog", Count: 3},
		},
	}

	// Get existing correction
	correction, found := profile.GetCorrection("use println")
	if !found {
		t.Error("GetCorrection should find existing pattern")
	}
	if correction != "use slog" {
		t.Errorf("correction = %q, want %q", correction, "use slog")
	}

	// Get non-existent correction
	_, found = profile.GetCorrection("nonexistent")
	if found {
		t.Error("GetCorrection should return false for non-existent pattern")
	}
}

func TestUserProfile_ThreadSafety(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	done := make(chan bool)

	// Concurrent writes
	go func() {
		for i := 0; i < 100; i++ {
			profile.SetPreference("key", "value1")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			profile.SetPreference("key", "value2")
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < 100; i++ {
			_ = profile.GetPreference("key")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			profile.RecordCorrection("pattern", "correction")
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// If we get here without a race detector error, the test passes
}

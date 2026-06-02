package executor

import (
	"strings"
	"testing"
)

// Test decomposition wiring in runner (GH-218)
func TestRunner_SetDecomposer(t *testing.T) {
	runner := NewRunner()

	// Initially no decomposer
	if runner.decomposer != nil {
		t.Error("Expected decomposer to be nil initially")
	}

	// Set decomposer
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 50,
	}
	decomposer := NewTaskDecomposer(config)
	runner.SetDecomposer(decomposer)

	if runner.decomposer == nil {
		t.Error("Expected decomposer to be set")
	}
	if runner.decomposer != decomposer {
		t.Error("Expected decomposer to be the one we set")
	}
}

func TestRunner_EnableDecomposition(t *testing.T) {
	runner := NewRunner()

	// Enable with nil config - should use defaults with enabled=true
	runner.EnableDecomposition(nil)

	if runner.decomposer == nil {
		t.Error("Expected decomposer to be created")
	}

	// Enable with custom config
	runner2 := NewRunner()
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "medium",
		MaxSubtasks:         3,
		MinDescriptionWords: 20,
	}
	runner2.EnableDecomposition(config)

	if runner2.decomposer == nil {
		t.Error("Expected decomposer to be created with custom config")
	}
}

func TestNewRunnerWithConfig_Decompose(t *testing.T) {
	// Test that NewRunnerWithConfig wires decomposer from config
	config := &BackendConfig{
		Type: "claude-code",
		ClaudeCode: &ClaudeCodeConfig{
			Command: "claude",
		},
		Decompose: &DecomposeConfig{
			Enabled:             true,
			MinComplexity:       "complex",
			MaxSubtasks:         5,
			MinDescriptionWords: 50,
		},
	}

	runner, err := NewRunnerWithConfig(config)
	if err != nil {
		t.Fatalf("NewRunnerWithConfig failed: %v", err)
	}

	if runner.decomposer == nil {
		t.Error("Expected decomposer to be wired from config")
	}
}

func TestNewRunnerWithConfig_DecomposeDisabled(t *testing.T) {
	// Test that disabled decompose config doesn't create decomposer
	config := &BackendConfig{
		Type: "claude-code",
		ClaudeCode: &ClaudeCodeConfig{
			Command: "claude",
		},
		Decompose: &DecomposeConfig{
			Enabled: false, // Disabled
		},
	}

	runner, err := NewRunnerWithConfig(config)
	if err != nil {
		t.Fatalf("NewRunnerWithConfig failed: %v", err)
	}

	if runner.decomposer != nil {
		t.Error("Expected decomposer to be nil when disabled in config")
	}
}

// Test runner with SkipSelfReview config (GH-364)
func TestNewRunnerWithConfig_SkipSelfReview(t *testing.T) {
	config := &BackendConfig{
		Type: "claude-code",
		ClaudeCode: &ClaudeCodeConfig{
			Command: "claude",
		},
		SkipSelfReview: true,
	}

	runner, err := NewRunnerWithConfig(config)
	if err != nil {
		t.Fatalf("NewRunnerWithConfig failed: %v", err)
	}

	if runner.config == nil {
		t.Fatal("Expected runner.config to be set")
	}
	if !runner.config.SkipSelfReview {
		t.Error("Expected SkipSelfReview to be true in runner config")
	}
}

// Test ExtractRepoName function (GH-386)
func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		repo     string
		expected string
	}{
		{"ylcn91/pilot", "pilot"},
		{"org/my-repo", "my-repo"},
		{"company/complex.repo.name", "complex.repo.name"},
		{"pilot", "pilot"}, // Already just repo name
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			result := ExtractRepoName(tt.repo)
			if result != tt.expected {
				t.Errorf("ExtractRepoName(%q) = %q, want %q", tt.repo, result, tt.expected)
			}
		})
	}
}

// Test ValidateRepoProjectMatch function (GH-386)
func TestValidateRepoProjectMatch(t *testing.T) {
	tests := []struct {
		name        string
		sourceRepo  string
		projectPath string
		wantErr     bool
	}{
		{
			name:        "matching repo and project",
			sourceRepo:  "ylcn91/pilot",
			projectPath: "/Users/test/Projects/pilot",
			wantErr:     false,
		},
		{
			name:        "matching with different case",
			sourceRepo:  "qf-studio/Pilot",
			projectPath: "/Users/test/Projects/pilot",
			wantErr:     false,
		},
		{
			name:        "mismatched repo and project",
			sourceRepo:  "ylcn91/pilot",
			projectPath: "/Users/test/Projects/bostonteamgroup",
			wantErr:     true,
		},
		{
			name:        "empty source repo",
			sourceRepo:  "",
			projectPath: "/Users/test/Projects/pilot",
			wantErr:     false, // No validation needed
		},
		{
			name:        "empty project path",
			sourceRepo:  "ylcn91/pilot",
			projectPath: "",
			wantErr:     false, // No validation needed
		},
		{
			name:        "both empty",
			sourceRepo:  "",
			projectPath: "",
			wantErr:     false,
		},
		{
			name:        "similar but not matching",
			sourceRepo:  "org/pilot-dev",
			projectPath: "/Projects/pilot",
			wantErr:     true,
		},
		{
			name:        "repo name with special chars",
			sourceRepo:  "org/my-awesome-project",
			projectPath: "/home/user/my-awesome-project",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepoProjectMatch(tt.sourceRepo, tt.projectPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRepoProjectMatch(%q, %q) error = %v, wantErr %v",
					tt.sourceRepo, tt.projectPath, err, tt.wantErr)
			}
		})
	}
}

// Test that Task struct has SourceRepo field (GH-386)
func TestTaskStructSourceRepo(t *testing.T) {
	task := &Task{
		ID:          "GH-386",
		Title:       "Cross-project defense",
		Description: "Prevent cross-project execution",
		ProjectPath: "/Users/test/Projects/pilot",
		Branch:      "pilot/GH-386",
		CreatePR:    true,
		SourceRepo:  "ylcn91/pilot",
	}

	if task.SourceRepo != "ylcn91/pilot" {
		t.Errorf("SourceRepo = %q, want ylcn91/pilot", task.SourceRepo)
	}
}

// Test mismatch error message format (GH-386)
func TestValidateRepoProjectMatchErrorMessage(t *testing.T) {
	err := ValidateRepoProjectMatch("ylcn91/pilot", "/Projects/wrong-project")
	if err == nil {
		t.Fatal("Expected error for mismatched repo/project")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ylcn91/pilot") {
		t.Error("Error message should contain source repo")
	}
	if !strings.Contains(errMsg, "wrong-project") {
		t.Error("Error message should contain project path")
	}
	if !strings.Contains(errMsg, "pilot") {
		t.Error("Error message should contain expected project name")
	}
}

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/quality"
)

// TestWorktreePathIntegration tests path handling in worktree mode without full execution.
// GH-970: This integration test verifies that DetectBuildCommand and qualityCheckerFactory
// receive worktree paths correctly when worktree mode is enabled.
func TestWorktreePathIntegration(t *testing.T) {
	// Setup test repo with project config files
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Create and commit project config files
	createProjectConfigFiles(t, localRepo)
	commitProjectFiles(t, localRepo)

	ctx := context.Background()

	// Test 1: Verify DetectBuildCommand works in worktree
	t.Run("DetectBuildCommand in worktree", func(t *testing.T) {
		manager := NewWorktreeManager(localRepo)
		result, err := manager.CreateWorktreeWithBranch(ctx, "detect-test", "pilot/detect-test", "main")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		defer result.Cleanup()

		// Verify project config files exist in worktree
		verifyProjectConfigInPath(t, result.Path)

		// Test DetectBuildCommand in worktree path
		buildCmd := quality.DetectBuildCommand(result.Path)
		expectedCmd := "go build ./..." // We created go.mod
		if buildCmd != expectedCmd {
			t.Errorf("DetectBuildCommand in worktree returned %q, expected %q", buildCmd, expectedCmd)
		}

		// Verify it's different from what we get with original repo path
		origCmd := quality.DetectBuildCommand(localRepo)
		if origCmd != expectedCmd {
			t.Errorf("DetectBuildCommand in original repo returned %q, expected %q", origCmd, expectedCmd)
		}

		// Both should be the same (same project files), but paths are different
		if buildCmd != origCmd {
			t.Errorf("Build commands should be identical: worktree=%q vs orig=%q", buildCmd, origCmd)
		}
	})

	// Test 2: Verify path isolation works
	t.Run("Path isolation verification", func(t *testing.T) {
		manager := NewWorktreeManager(localRepo)
		result, err := manager.CreateWorktreeWithBranch(ctx, "isolation-test", "pilot/isolation-test", "main")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		defer result.Cleanup()

		// Worktree path should be different from original repo
		if result.Path == localRepo {
			t.Errorf("Worktree path %q should be different from original repo %q", result.Path, localRepo)
		}

		// Worktree path should be in temp directory
		if !strings.Contains(result.Path, "pilot-worktree") {
			t.Errorf("Worktree path %q should contain 'pilot-worktree'", result.Path)
		}

		// Both paths should have the same project structure
		origFiles := getProjectFiles(t, localRepo)
		worktreeFiles := getProjectFiles(t, result.Path)

		if len(origFiles) != len(worktreeFiles) {
			t.Errorf("File count mismatch: original=%d, worktree=%d", len(origFiles), len(worktreeFiles))
		}

		for _, file := range []string{"go.mod", "package.json", "tsconfig.json", "Makefile"} {
			origPath := filepath.Join(localRepo, file)
			worktreePath := filepath.Join(result.Path, file)

			origExists := fileExists(origPath)
			worktreeExists := fileExists(worktreePath)

			if origExists != worktreeExists {
				t.Errorf("File %s existence mismatch: original=%v, worktree=%v", file, origExists, worktreeExists)
			}
		}
	})

	// Test 3: Test quality checker factory path handling (mock scenario)
	t.Run("Quality checker factory path", func(t *testing.T) {
		manager := NewWorktreeManager(localRepo)
		result, err := manager.CreateWorktreeWithBranch(ctx, "quality-test", "pilot/quality-test", "main")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		defer result.Cleanup()

		// Test the pattern that would be used in real execution
		var receivedPath string
		mockFactory := func(taskID, projectPath string) QualityChecker {
			receivedPath = projectPath

			// Verify we can detect project type in this path
			buildCmd := quality.DetectBuildCommand(projectPath)
			if buildCmd == "" {
				t.Errorf("DetectBuildCommand failed for path %q", projectPath)
			}

			return &mockQualityChecker{
				outcome: &QualityOutcome{Passed: true},
			}
		}

		// Simulate calling the factory with worktree path
		checker := mockFactory("quality-test", result.Path)
		if receivedPath != result.Path {
			t.Errorf("Factory received wrong path: got %q, expected %q", receivedPath, result.Path)
		}

		// Verify checker functionality
		outcome, err := checker.Check(ctx)
		if err != nil {
			t.Errorf("Mock checker failed: %v", err)
		}
		if !outcome.Passed {
			t.Error("Expected mock checker to pass")
		}
	})

	// Test 4: Navigator copy functionality
	t.Run("Navigator copy to worktree", func(t *testing.T) {
		// Create .agent directory in original repo
		agentDir := filepath.Join(localRepo, ".agent")
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			t.Fatalf("Failed to create .agent dir: %v", err)
		}

		// Create Navigator files (these might be gitignored in real scenarios)
		devReadme := filepath.Join(agentDir, "DEVELOPMENT-README.md")
		if err := os.WriteFile(devReadme, []byte("# Navigator Config\nProject-specific config"), 0644); err != nil {
			t.Fatalf("Failed to create dev readme: %v", err)
		}

		markersDir := filepath.Join(agentDir, ".context-markers")
		if err := os.MkdirAll(markersDir, 0755); err != nil {
			t.Fatalf("Failed to create markers dir: %v", err)
		}

		markerFile := filepath.Join(markersDir, "test-marker.md")
		if err := os.WriteFile(markerFile, []byte("# Test Context Marker\n"), 0644); err != nil {
			t.Fatalf("Failed to create marker file: %v", err)
		}

		manager := NewWorktreeManager(localRepo)
		result, err := manager.CreateWorktreeWithBranch(ctx, "nav-test", "pilot/nav-test", "main")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		defer result.Cleanup()

		// Copy Navigator config to worktree
		if err := EnsureNavigatorInWorktree(localRepo, result.Path); err != nil {
			t.Fatalf("EnsureNavigatorInWorktree failed: %v", err)
		}

		// Verify Navigator files were copied to worktree
		worktreeDevReadme := filepath.Join(result.Path, ".agent", "DEVELOPMENT-README.md")
		if !fileExists(worktreeDevReadme) {
			t.Error("Navigator DEVELOPMENT-README.md not found in worktree")
		}

		worktreeMarker := filepath.Join(result.Path, ".agent", ".context-markers", "test-marker.md")
		if !fileExists(worktreeMarker) {
			t.Error("Navigator marker file not found in worktree")
		}

		// Verify content is correct
		content, err := os.ReadFile(worktreeDevReadme)
		if err != nil {
			t.Fatalf("Failed to read worktree dev readme: %v", err)
		}
		if !strings.Contains(string(content), "Navigator Config") {
			t.Errorf("Unexpected content in worktree dev readme: %s", content)
		}
	})
}

// TestWorktreeVsOriginalDetection compares DetectBuildCommand results between worktree and original repo
func TestWorktreeVsOriginalDetection(t *testing.T) {
	testCases := []struct {
		name          string
		configFile    string
		configContent string
		expectedCmd   string
	}{
		{
			name:          "Go project",
			configFile:    "go.mod",
			configContent: "module github.com/test/project\n\ngo 1.24\n",
			expectedCmd:   "go build ./...",
		},
		{
			name:          "Node.js with TypeScript",
			configFile:    "package.json",
			configContent: `{"name": "test", "dependencies": {}}`,
			expectedCmd:   "npm run build || npx tsc --noEmit",
		},
		{
			name:          "Rust project",
			configFile:    "Cargo.toml",
			configContent: "[package]\nname = \"test\"\nversion = \"0.1.0\"\n",
			expectedCmd:   "cargo check",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test repo
			localRepo, remoteRepo := setupTestRepoWithRemote(t)
			defer func() { _ = os.RemoveAll(localRepo) }()
			defer func() { _ = os.RemoveAll(remoteRepo) }()

			// Create project config file
			configPath := filepath.Join(localRepo, tc.configFile)
			if err := os.WriteFile(configPath, []byte(tc.configContent), 0644); err != nil {
				t.Fatalf("Failed to create config file: %v", err)
			}

			// For TypeScript test, also create tsconfig.json
			if tc.name == "Node.js with TypeScript" {
				tsConfigPath := filepath.Join(localRepo, "tsconfig.json")
				tsConfig := `{"compilerOptions": {"strict": true}}`
				if err := os.WriteFile(tsConfigPath, []byte(tsConfig), 0644); err != nil {
					t.Fatalf("Failed to create tsconfig.json: %v", err)
				}
			}

			// Commit files so they appear in worktree
			if err := runGitCommand(localRepo, "add", "."); err != nil {
				t.Fatalf("Failed to git add: %v", err)
			}
			if err := runGitCommand(localRepo, "commit", "-m", "Add config files"); err != nil {
				t.Fatalf("Failed to git commit: %v", err)
			}

			// Test detection in original repo
			origCmd := quality.DetectBuildCommand(localRepo)
			if origCmd != tc.expectedCmd {
				t.Errorf("Original repo: expected %q, got %q", tc.expectedCmd, origCmd)
			}

			ctx := context.Background()
			manager := NewWorktreeManager(localRepo)

			// Test detection in worktree
			result, err := manager.CreateWorktree(ctx, "detect-test")
			if err != nil {
				t.Fatalf("Failed to create worktree: %v", err)
			}
			defer result.Cleanup()

			worktreeCmd := quality.DetectBuildCommand(result.Path)
			if worktreeCmd != tc.expectedCmd {
				t.Errorf("Worktree: expected %q, got %q", tc.expectedCmd, worktreeCmd)
			}

			// Both should detect the same command
			if origCmd != worktreeCmd {
				t.Errorf("Command mismatch: original=%q, worktree=%q", origCmd, worktreeCmd)
			}

			// Verify config file exists in both locations
			origConfigExists := fileExists(filepath.Join(localRepo, tc.configFile))
			worktreeConfigExists := fileExists(filepath.Join(result.Path, tc.configFile))

			if !origConfigExists {
				t.Error("Config file missing from original repo")
			}
			if !worktreeConfigExists {
				t.Error("Config file missing from worktree")
			}
		})
	}
}

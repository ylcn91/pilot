package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Helper functions

// getProjectFiles returns a list of files in the given directory
func getProjectFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Failed to read directory %s: %v", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".git") {
			files = append(files, entry.Name())
		}
	}
	return files
}

// fileExists returns true if the file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// createProjectConfigFiles creates various project configuration files for testing
func createProjectConfigFiles(t *testing.T, repoPath string) {
	t.Helper()

	// Create go.mod for Go detection
	goMod := filepath.Join(repoPath, "go.mod")
	goModContent := "module github.com/test/project\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create package.json for Node.js detection
	packageJson := filepath.Join(repoPath, "package.json")
	packageContent := `{
	"name": "test-project",
	"version": "1.0.0",
	"scripts": {
		"build": "webpack --mode=production",
		"test": "jest"
	},
	"dependencies": {
		"react": "^18.0.0"
	}
}`
	if err := os.WriteFile(packageJson, []byte(packageContent), 0644); err != nil {
		t.Fatalf("Failed to create package.json: %v", err)
	}

	// Create tsconfig.json for TypeScript detection
	tsConfig := filepath.Join(repoPath, "tsconfig.json")
	tsContent := `{
	"compilerOptions": {
		"target": "ES2020",
		"lib": ["ES2020"],
		"module": "commonjs",
		"strict": true
	}
}`
	if err := os.WriteFile(tsConfig, []byte(tsContent), 0644); err != nil {
		t.Fatalf("Failed to create tsconfig.json: %v", err)
	}

	// Create Makefile for build commands
	makefile := filepath.Join(repoPath, "Makefile")
	makeContent := `build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run
`
	if err := os.WriteFile(makefile, []byte(makeContent), 0644); err != nil {
		t.Fatalf("Failed to create Makefile: %v", err)
	}
}

// commitProjectFiles commits the project files to git so they appear in worktrees
func commitProjectFiles(t *testing.T, repoPath string) {
	t.Helper()

	// Add all files
	if err := runGitCommand(repoPath, "add", "."); err != nil {
		t.Fatalf("Failed to git add files: %v", err)
	}

	// Commit files
	if err := runGitCommand(repoPath, "commit", "-m", "Add project config files"); err != nil {
		t.Fatalf("Failed to git commit files: %v", err)
	}
}

// runGitCommand runs a git command in the specified directory
func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

// verifyProjectConfigInPath verifies that project config files exist in the given path
func verifyProjectConfigInPath(t *testing.T, projectPath string) {
	t.Helper()

	configFiles := []string{"go.mod", "package.json", "tsconfig.json", "Makefile"}

	for _, file := range configFiles {
		path := filepath.Join(projectPath, file)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Project config file %s not found in path %s: %v", file, projectPath, err)
		}
	}
}

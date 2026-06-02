package quality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectBuildCommand(t *testing.T) {
	// Create temp directory for tests
	tmpDir, err := os.MkdirTemp("", "quality-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name        string
		setupFiles  []string
		expectedCmd string
	}{
		{
			name:        "go project",
			setupFiles:  []string{"go.mod"},
			expectedCmd: "go build ./...",
		},
		{
			name:        "typescript project",
			setupFiles:  []string{"package.json", "tsconfig.json"},
			expectedCmd: "npm run build || npx tsc --noEmit",
		},
		{
			name:        "node project without typescript",
			setupFiles:  []string{"package.json"},
			expectedCmd: "npm run build --if-present",
		},
		{
			name:        "rust project",
			setupFiles:  []string{"Cargo.toml"},
			expectedCmd: "cargo check",
		},
		{
			name:        "python project with pyproject",
			setupFiles:  []string{"pyproject.toml"},
			expectedCmd: "python -m py_compile $(find . -name '*.py' -not -path './venv/*' -not -path './.venv/*' 2>/dev/null | head -100)",
		},
		{
			name:        "python project with setup.py",
			setupFiles:  []string{"setup.py"},
			expectedCmd: "python -m py_compile $(find . -name '*.py' -not -path './venv/*' -not -path './.venv/*' 2>/dev/null | head -100)",
		},
		{
			name:        "unknown project",
			setupFiles:  []string{},
			expectedCmd: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a subdirectory for this test
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.MkdirAll(testDir, 0755); err != nil {
				t.Fatalf("failed to create test dir: %v", err)
			}

			// Create the setup files
			for _, f := range tt.setupFiles {
				filePath := filepath.Join(testDir, f)
				if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
					t.Fatalf("failed to create file %s: %v", f, err)
				}
			}

			// Test detection
			got := DetectBuildCommand(testDir)
			if got != tt.expectedCmd {
				t.Errorf("DetectBuildCommand() = %q, want %q", got, tt.expectedCmd)
			}
		})
	}
}

func TestDetectTestCommand(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "quality-test-detect-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name        string
		files       map[string]string // relative path → content
		expectedCmd string
	}{
		{
			name:        "make with test target",
			files:       map[string]string{"Makefile": "build:\n\techo build\n\ntest:\n\techo run tests\n"},
			expectedCmd: "make test",
		},
		{
			name:        "make without test target",
			files:       map[string]string{"Makefile": "build:\n\techo build\n", "go.mod": ""},
			expectedCmd: "go test ./...",
		},
		{
			name:        "pytest via py file",
			files:       map[string]string{"app.py": "print('hi')\n"},
			expectedCmd: "pytest -v 2>&1",
		},
		{
			name:        "pytest via pyproject",
			files:       map[string]string{"pyproject.toml": ""},
			expectedCmd: "pytest -v 2>&1",
		},
		{
			name:        "npm test",
			files:       map[string]string{"package.json": "{}"},
			expectedCmd: "npm test",
		},
		{
			name:        "cargo test",
			files:       map[string]string{"Cargo.toml": ""},
			expectedCmd: "cargo test",
		},
		{
			name:        "go test",
			files:       map[string]string{"go.mod": "module x\n"},
			expectedCmd: "go test ./...",
		},
		{
			name:        "empty workspace",
			files:       map[string]string{},
			expectedCmd: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.MkdirAll(testDir, 0755); err != nil {
				t.Fatalf("failed to create test dir: %v", err)
			}
			for name, content := range tt.files {
				path := filepath.Join(testDir, name)
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create file %s: %v", name, err)
				}
			}

			got := DetectTestCommand(testDir)
			if got != tt.expectedCmd {
				t.Errorf("DetectTestCommand() = %q, want %q", got, tt.expectedCmd)
			}
		})
	}
}

func TestDetectBuildCommand_Priority(t *testing.T) {
	// Test that Go takes priority when multiple indicators exist
	tmpDir, err := os.MkdirTemp("", "quality-priority-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create both go.mod and package.json
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(""), 0644)

	// Go should take priority
	got := DetectBuildCommand(tmpDir)
	if got != "go build ./..." {
		t.Errorf("expected Go build command to take priority, got %q", got)
	}
}

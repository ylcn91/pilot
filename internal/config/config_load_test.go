package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("MissingFile", func(t *testing.T) {
		config, err := Load("/nonexistent/path/config.yaml")
		if err != nil {
			t.Errorf("Load should return defaults for missing file, got error: %v", err)
		}
		if config == nil {
			t.Fatal("Load returned nil config for missing file")
		}
		// Should return default config
		if config.Version != "1.0" {
			t.Errorf("Version = %q, want default %q", config.Version, "1.0")
		}
	})

	t.Run("ValidConfigFile", func(t *testing.T) {
		// Create temp config file
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "2.0"
gateway:
  host: "0.0.0.0"
  port: 8080
  codex_runtime:
    command: "/usr/local/bin/codex"
    args:
      - "app-server"
      - "--stdio"
    model: "gpt-5.1-codex"
    sandbox: "workspace-write"
orchestrator:
  model: "claude-opus"
  max_concurrent: 4
memory:
  path: "/custom/path"
  cross_project: false
projects:
  - name: "test-project"
    path: "/path/to/project"
    navigator: true
    default_branch: "develop"
default_project: "test-project"
dashboard:
  refresh_interval: 500
  show_logs: false
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		config, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if config.Version != "2.0" {
			t.Errorf("Version = %q, want %q", config.Version, "2.0")
		}
		if config.Gateway.Host != "0.0.0.0" {
			t.Errorf("Gateway.Host = %q, want %q", config.Gateway.Host, "0.0.0.0")
		}
		if config.Gateway.Port != 8080 {
			t.Errorf("Gateway.Port = %d, want %d", config.Gateway.Port, 8080)
		}
		if config.Gateway.CodexRuntime == nil {
			t.Fatal("Gateway.CodexRuntime is nil")
		}
		if config.Gateway.CodexRuntime.Command != "/usr/local/bin/codex" {
			t.Errorf("Gateway.CodexRuntime.Command = %q, want %q", config.Gateway.CodexRuntime.Command, "/usr/local/bin/codex")
		}
		if len(config.Gateway.CodexRuntime.Args) != 2 || config.Gateway.CodexRuntime.Args[1] != "--stdio" {
			t.Errorf("Gateway.CodexRuntime.Args = %v, want %v", config.Gateway.CodexRuntime.Args, []string{"app-server", "--stdio"})
		}
		if config.Gateway.CodexRuntime.Model != "gpt-5.1-codex" {
			t.Errorf("Gateway.CodexRuntime.Model = %q, want %q", config.Gateway.CodexRuntime.Model, "gpt-5.1-codex")
		}
		if config.Gateway.CodexRuntime.Sandbox != "workspace-write" {
			t.Errorf("Gateway.CodexRuntime.Sandbox = %q, want %q", config.Gateway.CodexRuntime.Sandbox, "workspace-write")
		}
		if config.Orchestrator.Model != "claude-opus" {
			t.Errorf("Orchestrator.Model = %q, want %q", config.Orchestrator.Model, "claude-opus")
		}
		if config.Orchestrator.MaxConcurrent != 4 {
			t.Errorf("Orchestrator.MaxConcurrent = %d, want %d", config.Orchestrator.MaxConcurrent, 4)
		}
		if config.Memory.Path != "/custom/path" {
			t.Errorf("Memory.Path = %q, want %q", config.Memory.Path, "/custom/path")
		}
		if config.Memory.CrossProject != false {
			t.Error("Memory.CrossProject should be false")
		}
		if len(config.Projects) != 1 {
			t.Fatalf("Projects length = %d, want 1", len(config.Projects))
		}
		if config.Projects[0].Name != "test-project" {
			t.Errorf("Projects[0].Name = %q, want %q", config.Projects[0].Name, "test-project")
		}
		if config.DefaultProject != "test-project" {
			t.Errorf("DefaultProject = %q, want %q", config.DefaultProject, "test-project")
		}
		if config.Dashboard.RefreshInterval != 500 {
			t.Errorf("Dashboard.RefreshInterval = %d, want %d", config.Dashboard.RefreshInterval, 500)
		}
		if config.Dashboard.ShowLogs != false {
			t.Error("Dashboard.ShowLogs should be false")
		}
	})

	t.Run("EnvironmentVariableExpansion", func(t *testing.T) {
		// Set test environment variable
		testValue := "my-secret-token"
		t.Setenv("TEST_LINEAR_TOKEN", testValue)

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "1.0"
adapters:
  linear:
    enabled: true
    api_key: "${TEST_LINEAR_TOKEN}"
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		config, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if config.Adapters.Linear.APIKey != testValue {
			t.Errorf("Linear.APIKey = %q, want %q (env var expansion failed)", config.Adapters.Linear.APIKey, testValue)
		}
	})

	t.Run("PathExpansionTilde", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "1.0"
memory:
  path: "~/custom/pilot/data"
projects:
  - name: "home-project"
    path: "~/projects/myapp"
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		config, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		homeDir, _ := os.UserHomeDir()

		expectedMemoryPath := filepath.Join(homeDir, "custom/pilot/data")
		if config.Memory.Path != expectedMemoryPath {
			t.Errorf("Memory.Path = %q, want %q", config.Memory.Path, expectedMemoryPath)
		}

		expectedProjectPath := filepath.Join(homeDir, "projects/myapp")
		if config.Projects[0].Path != expectedProjectPath {
			t.Errorf("Projects[0].Path = %q, want %q", config.Projects[0].Path, expectedProjectPath)
		}
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `
version: "1.0"
gateway:
  host: [invalid yaml structure
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("Load should fail for invalid YAML")
		}
	})

	t.Run("UnreadableFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		if err := os.WriteFile(configPath, []byte("version: 1.0"), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		// Make file unreadable
		if err := os.Chmod(configPath, 0000); err != nil {
			t.Skipf("Cannot change file permissions: %v", err)
		}
		defer func() { _ = os.Chmod(configPath, 0644) }() // Restore permissions for cleanup

		_, err := Load(configPath)
		if err == nil {
			t.Error("Load should fail for unreadable file")
		}
	})
}

func TestExpandPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "TildeOnly",
			input:    "~",
			expected: homeDir,
		},
		{
			name:     "TildeWithPath",
			input:    "~/path/to/file",
			expected: filepath.Join(homeDir, "path/to/file"),
		},
		{
			name:     "TildeWithSlash",
			input:    "~/",
			expected: filepath.Join(homeDir, ""),
		},
		{
			name:     "AbsolutePath",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "RelativePath",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "EmptyPath",
			input:    "",
			expected: "",
		},
		{
			name:     "TildeInMiddle",
			input:    "/path/~/with/tilde",
			expected: "/path/~/with/tilde", // Should not expand ~ in middle
		},
		{
			name:     "DoubleSlash",
			input:    "~//double/slash",
			expected: filepath.Join(homeDir, "/double/slash"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if result != tt.expected {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

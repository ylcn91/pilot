package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSave(t *testing.T) {
	t.Run("SaveToNewFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "subdir", "config.yaml")

		config := DefaultConfig()
		config.Version = "test-version"
		config.Gateway.Port = 9999

		err := Save(config, configPath)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// Verify file was created
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("Config file was not created")
		}

		// Load it back and verify
		loadedConfig, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if loadedConfig.Version != "test-version" {
			t.Errorf("Version = %q, want %q", loadedConfig.Version, "test-version")
		}
		if loadedConfig.Gateway.Port != 9999 {
			t.Errorf("Gateway.Port = %d, want %d", loadedConfig.Gateway.Port, 9999)
		}
	})

	t.Run("SaveToExistingFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		// Create initial config
		initialConfig := DefaultConfig()
		initialConfig.Version = "initial"
		if err := Save(initialConfig, configPath); err != nil {
			t.Fatalf("Initial save failed: %v", err)
		}

		// Save updated config
		updatedConfig := DefaultConfig()
		updatedConfig.Version = "updated"
		if err := Save(updatedConfig, configPath); err != nil {
			t.Fatalf("Updated save failed: %v", err)
		}

		// Verify it was overwritten
		loadedConfig, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loadedConfig.Version != "updated" {
			t.Errorf("Version = %q, want %q", loadedConfig.Version, "updated")
		}
	})

	t.Run("SaveToUnwritableDirectory", func(t *testing.T) {
		// Try to save to a path we can't write to
		err := Save(DefaultConfig(), "/root/unwritable/config.yaml")
		if err == nil {
			// On some systems this might work if running as root
			t.Skip("Unable to test unwritable directory (might be running as root)")
		}
	})
}

// TestSave_PermissionsAre0600 asserts that Save() writes the config file
// with 0600 permissions and its parent directory with 0700 — required
// because the file stores GitHub PAT, Linear API key, Slack bot token,
// and (optionally) Anthropic API key. TASK-290.
//
// Skipped on Windows where Unix file modes don't apply.
func TestSave_PermissionsAre0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".pilot")
	configPath := filepath.Join(configDir, "config.yaml")

	cfg := &Config{Version: "1.0"}
	if err := Save(cfg, configPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat config file failed: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0600 {
		t.Errorf("config file mode = %o, want 0600", got)
	}

	dirInfo, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("Stat config dir failed: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0700 {
		t.Errorf("config dir mode = %o, want 0700", got)
	}
}

// TestSave_TightensExistingLoosePerms asserts that Save() rewrites a file
// that previously existed with 0644 down to 0600. Covers the migration path
// for users upgrading past TASK-290 with an existing 0644 config on disk.
func TestSave_TightensExistingLoosePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Pre-seed file at 0644 (the old default).
	if err := os.WriteFile(configPath, []byte("version: \"1.0\"\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	cfg := &Config{Version: "1.0"}
	if err := Save(cfg, configPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat after Save failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("config file mode after rewrite = %o, want 0600", got)
	}
}

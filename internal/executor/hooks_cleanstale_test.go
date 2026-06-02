package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanStalePilotHooks(t *testing.T) {
	type testCase struct {
		name      string
		buildJSON func(scriptDir string) string
		validate  func(t *testing.T, settingsPath string, scriptDir string)
	}

	tests := []testCase{
		{
			name:      "no-op when settings file does not exist",
			buildJSON: func(scriptDir string) string { return "" },
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				if _, err := os.ReadFile(settingsPath); !os.IsNotExist(err) {
					t.Error("Expected no file to exist")
				}
			},
		},
		{
			name: "removes stale pilot entries with dead script paths",
			buildJSON: func(scriptDir string) string {
				// scriptDir exists but scripts are NOT written — dead paths
				return `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` +
					filepath.Join(scriptDir, "pilot-bash-guard.sh") + `"}]}],"Stop":[{"hooks":[{"type":"command","command":"` +
					filepath.Join(scriptDir, "pilot-stop-gate.sh") + `"}]}]}}`
			},
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				if _, hasHooks := parsed["hooks"]; hasHooks {
					t.Error("Expected hooks key removed after all stale entries cleaned")
				}
			},
		},
		{
			name: "removes entries with old object-format matchers",
			buildJSON: func(scriptDir string) string {
				// Write the script so it exists — only the matcher format should trigger removal
				_ = os.WriteFile(filepath.Join(scriptDir, "pilot-bash-guard.sh"), []byte("#!/bin/sh\n"), 0755)
				return `{"hooks":{"PreToolUse":[{"matcher":{"tools":["Bash"]},"hooks":[{"type":"command","command":"` +
					filepath.Join(scriptDir, "pilot-bash-guard.sh") + `"}]}]}}`
			},
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				if _, hasHooks := parsed["hooks"]; hasHooks {
					t.Error("Expected hooks key removed after object-format entry cleaned")
				}
			},
		},
		{
			name: "preserves user (non-pilot) hooks even when hooks disabled",
			buildJSON: func(scriptDir string) string {
				// Write user hook and stale pilot hook
				_ = os.WriteFile(filepath.Join(scriptDir, "my-hook.sh"), []byte("#!/bin/sh\n"), 0755)
				// pilot-bash-guard.sh NOT written — stale
				return `{"hooks":{"PreToolUse":[` +
					`{"matcher":"Bash","hooks":[{"type":"command","command":"` + filepath.Join(scriptDir, "pilot-bash-guard.sh") + `"}]},` +
					`{"matcher":"Bash","hooks":[{"type":"command","command":"` + filepath.Join(scriptDir, "my-hook.sh") + `"}]}` +
					`]}}`
			},
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				hooks, ok := parsed["hooks"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected hooks to remain")
				}
				preArr, ok := hooks["PreToolUse"].([]interface{})
				if !ok || len(preArr) != 1 {
					t.Errorf("Expected 1 PreToolUse entry (user hook), got %v", hooks["PreToolUse"])
				}
			},
		},
		{
			name: "removes hooks key entirely when all entries are stale",
			buildJSON: func(scriptDir string) string {
				return `{"other":"value","hooks":{"Stop":[{"hooks":[{"type":"command","command":"` +
					filepath.Join(scriptDir, "pilot-stop-gate.sh") + `"}]}]}}`
			},
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				if _, hasHooks := parsed["hooks"]; hasHooks {
					t.Error("Expected hooks key removed")
				}
				if parsed["other"] != "value" {
					t.Error("Expected other fields preserved")
				}
			},
		},
		{
			name: "no-op when settings has no stale entries",
			buildJSON: func(scriptDir string) string {
				_ = os.WriteFile(filepath.Join(scriptDir, "pilot-stop-gate.sh"), []byte("#!/bin/sh\n"), 0755)
				return `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` +
					filepath.Join(scriptDir, "pilot-stop-gate.sh") + `"}]}]}}`
			},
			validate: func(t *testing.T, settingsPath string, scriptDir string) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("ReadFile: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				hooks, ok := parsed["hooks"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected hooks to remain")
				}
				if _, hasStop := hooks["Stop"]; !hasStop {
					t.Error("Expected Stop hook preserved when script exists")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
			scriptDir := t.TempDir()

			settingsJSON := tc.buildJSON(scriptDir)
			if settingsJSON != "" {
				if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.WriteFile(settingsPath, []byte(settingsJSON), 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			if err := CleanStalePilotHooks(settingsPath); err != nil {
				t.Fatalf("CleanStalePilotHooks: %v", err)
			}

			tc.validate(t, settingsPath, scriptDir)
		})
	}
}

func TestCleanStalePilotHooks_DualPathCleanup(t *testing.T) {
	// Simulates GH-1884: stale entries exist in both project root and worktree.
	// CleanStalePilotHooks called on each path independently should clean both.
	projectDir := t.TempDir()
	worktreeDir := t.TempDir()

	// Both paths have stale pilot hooks (scripts don't exist on disk)
	staleJSON := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/tmp/gone/pilot-stop-gate.sh"}]}]}}`
	for _, dir := range []string{projectDir, worktreeDir} {
		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(settingsPath, []byte(staleJSON), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Clean project root
	if err := CleanStalePilotHooks(filepath.Join(projectDir, ".claude", "settings.json")); err != nil {
		t.Fatalf("CleanStalePilotHooks (project root): %v", err)
	}
	// Clean worktree
	if err := CleanStalePilotHooks(filepath.Join(worktreeDir, ".claude", "settings.json")); err != nil {
		t.Fatalf("CleanStalePilotHooks (worktree): %v", err)
	}

	// Both should have hooks key removed
	for _, dir := range []string{projectDir, worktreeDir} {
		data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if _, hasHooks := parsed["hooks"]; hasHooks {
			t.Errorf("Expected hooks removed in %s, but still present", dir)
		}
	}
}

func TestRestoreUsesCleanupInsteadOfBlindRestore(t *testing.T) {
	// Verifies GH-1884: after hooks are installed, cleanup should use
	// CleanStalePilotHooks rather than restoring originalData which may
	// itself contain stale entries from a previous crash.

	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")

	// Simulate "original" settings that already have stale pilot entries
	// (from a previous crash — scripts don't exist on disk)
	staleOriginal := `{"other":"keep","hooks":{"Stop":[{"hooks":[{"type":"command","command":"/tmp/old-crash/pilot-stop-gate.sh"}]}]}}`
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(staleOriginal), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fresh pilot hooks (scripts exist)
	scriptDir := t.TempDir()
	for _, name := range []string{"pilot-stop-gate.sh", "pilot-bash-guard.sh"} {
		if err := os.WriteFile(filepath.Join(scriptDir, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	config := &HooksConfig{Enabled: true}
	hookSettings := GenerateClaudeSettings(config, scriptDir)

	// Merge (this captures the stale originalData internally)
	_, mergeErr := MergeWithExisting(settingsPath, hookSettings)
	if mergeErr != nil {
		t.Fatalf("MergeWithExisting: %v", mergeErr)
	}

	// Now simulate what the new hookRestoreFunc does: CleanStalePilotHooks
	// instead of calling the blind restoreFunc
	if err := CleanStalePilotHooks(settingsPath); err != nil {
		t.Fatalf("CleanStalePilotHooks: %v", err)
	}

	// Remove script dir to make current pilot entries stale too
	if err := os.RemoveAll(scriptDir); err != nil {
		t.Fatal(err)
	}

	// Run cleanup again (simulates what happens after scriptDir removal)
	if err := CleanStalePilotHooks(settingsPath); err != nil {
		t.Fatalf("CleanStalePilotHooks (second pass): %v", err)
	}

	// Verify: no stale pilot hooks remain, but "other" field is preserved
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, hasHooks := parsed["hooks"]; hasHooks {
		t.Error("Expected all stale pilot hooks removed, but hooks key still present")
	}
	if parsed["other"] != "keep" {
		t.Error("Expected non-hook fields preserved")
	}
}

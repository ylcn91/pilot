package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeWithExisting(t *testing.T) {
	tests := []struct {
		name           string
		existingJSON   string
		pilotSettings  map[string]interface{}
		expectError    bool
		validateResult func(t *testing.T, settingsPath string, restoreFunc func() error)
	}{
		{
			name:         "no existing file",
			existingJSON: "",
			pilotSettings: map[string]interface{}{
				"hooks": map[string][]HookMatcherEntry{
					"Stop": {
						{Hooks: []HookCommand{{Type: "command", Command: "/test/stop.sh"}}},
					},
				},
			},
			validateResult: func(t *testing.T, settingsPath string, restoreFunc func() error) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("Failed to read merged file: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Failed to unmarshal: %v", err)
				}
				if _, ok := parsed["hooks"]; !ok {
					t.Error("Expected hooks in merged file")
				}
				// Test restore removes the file
				if err := restoreFunc(); err != nil {
					t.Errorf("Restore failed: %v", err)
				}
				if _, err := os.ReadFile(settingsPath); !os.IsNotExist(err) {
					t.Error("Expected file to be removed after restore")
				}
			},
		},
		{
			name:         "existing file with old format hooks - replace",
			existingJSON: `{"other": "value", "hooks": {"Existing": {"command": "/existing.sh"}}}`,
			pilotSettings: map[string]interface{}{
				"hooks": map[string][]HookMatcherEntry{
					"Stop": {
						{Hooks: []HookCommand{{Type: "command", Command: "/test/stop.sh"}}},
					},
				},
			},
			validateResult: func(t *testing.T, settingsPath string, restoreFunc func() error) {
				data, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatalf("Failed to read: %v", err)
				}
				var parsed map[string]interface{}
				if err := json.Unmarshal(data, &parsed); err != nil {
					t.Fatalf("Failed to unmarshal: %v", err)
				}
				if parsed["other"] != "value" {
					t.Error("Expected existing 'other' field preserved")
				}
				hooks := parsed["hooks"].(map[string]interface{})
				if _, hasStop := hooks["Stop"]; !hasStop {
					t.Error("Expected Stop hook from pilot")
				}
				if err := restoreFunc(); err != nil {
					t.Errorf("Restore failed: %v", err)
				}
			},
		},
		{
			name:          "empty pilot settings is no-op",
			existingJSON:  `{"other": "value"}`,
			pilotSettings: map[string]interface{}{},
			validateResult: func(t *testing.T, settingsPath string, _ func() error) {
				data, _ := os.ReadFile(settingsPath)
				if string(data) != `{"other": "value"}` {
					t.Error("Expected file unchanged for empty pilot settings")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			settingsPath := filepath.Join(tempDir, ".claude", "settings.json")

			if tt.existingJSON != "" {
				if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
					t.Fatalf("Failed to create dir: %v", err)
				}
				if err := os.WriteFile(settingsPath, []byte(tt.existingJSON), 0644); err != nil {
					t.Fatalf("Failed to write: %v", err)
				}
			}

			restoreFunc, err := MergeWithExisting(settingsPath, tt.pilotSettings)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectError && tt.validateResult != nil {
				tt.validateResult(t, settingsPath, restoreFunc)
			}
		})
	}
}

func TestMergeNewFormatHooks_DeduplicatesStalePilotEntries(t *testing.T) {
	// Simulate the crash scenario: settings.json has stale pilot entries
	// from a previous run's temp dir, plus a user-defined hook.
	// Fresh pilot hooks should replace all stale pilot entries.

	// Create two temp dirs to simulate old and new pilot runs
	oldDir := t.TempDir()
	newDir := t.TempDir()

	// Write scripts to both dirs so hookFileExists returns true
	for _, dir := range []string{oldDir, newDir} {
		for _, name := range []string{"pilot-bash-guard.sh", "pilot-stop-gate.sh"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Also create a user hook script
	userDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(userDir, "my-custom-hook.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Existing settings: 3 stale pilot entries + 1 user entry for PreToolUse
	existing := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"matcher": "Bash",
				"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": filepath.Join(oldDir, "pilot-bash-guard.sh")},
				},
			},
			map[string]interface{}{
				"matcher": "Bash",
				"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": filepath.Join(userDir, "my-custom-hook.sh")},
				},
			},
		},
		"Stop": []interface{}{
			map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": filepath.Join(oldDir, "pilot-stop-gate.sh")},
				},
			},
		},
	}

	// Fresh pilot hooks (new temp dir)
	bashMatcher := "Bash"
	pilot := map[string][]HookMatcherEntry{
		"PreToolUse": {
			{
				Matcher: &bashMatcher,
				Hooks:   []HookCommand{{Type: "command", Command: filepath.Join(newDir, "pilot-bash-guard.sh")}},
			},
		},
		"Stop": {
			{
				Hooks: []HookCommand{{Type: "command", Command: filepath.Join(newDir, "pilot-stop-gate.sh")}},
			},
		},
	}

	merged := mergeNewFormatHooks(existing, pilot)

	// PreToolUse should have exactly 2 entries: fresh pilot + user hook
	preEntries := merged["PreToolUse"]
	var preCount int
	switch v := preEntries.(type) {
	case []HookMatcherEntry:
		preCount = len(v)
	case []interface{}:
		preCount = len(v)
	}
	if preCount != 2 {
		t.Errorf("PreToolUse: expected 2 entries (1 fresh pilot + 1 user), got %d", preCount)
	}

	// Stop should have exactly 1 entry: fresh pilot only
	stopEntries := merged["Stop"]
	var stopCount int
	switch v := stopEntries.(type) {
	case []HookMatcherEntry:
		stopCount = len(v)
	case []interface{}:
		stopCount = len(v)
	}
	if stopCount != 1 {
		t.Errorf("Stop: expected 1 entry (fresh pilot only), got %d", stopCount)
	}
}

func TestIsPilotManagedHook(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"/var/folders/xx/T/pilot-hooks-123/pilot-bash-guard.sh", true},
		{"/var/folders/xx/T/pilot-hooks-456/pilot-stop-gate.sh", true},
		{"/var/folders/xx/T/pilot-hooks-789/pilot-lint.sh", true},
		{"/home/user/.config/my-custom-hook.sh", false},
		{"/usr/local/bin/lint.sh", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isPilotManagedHook(tt.cmd); got != tt.expected {
				t.Errorf("isPilotManagedHook(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}

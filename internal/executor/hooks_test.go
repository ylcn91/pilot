package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHooksConfig_Defaults(t *testing.T) {
	config := DefaultHooksConfig()

	if config.Enabled {
		t.Error("Expected hooks to be disabled by default")
	}
	// GH-2432: RunTestsOnStop default flipped to false to cut subprocess token spend.
	if config.RunTestsOnStop == nil || *config.RunTestsOnStop {
		t.Error("Expected RunTestsOnStop to default to false (GH-2432)")
	}
	if config.BlockDestructive == nil || !*config.BlockDestructive {
		t.Error("Expected BlockDestructive to default to true when enabled")
	}
	if config.LintOnSave {
		t.Error("Expected LintOnSave to default to false")
	}
}

func TestGenerateClaudeSettings(t *testing.T) {
	tests := []struct {
		name       string
		config     *HooksConfig
		expectKeys int // number of hook types expected
	}{
		{"nil config", nil, 0},
		{"disabled config", &HooksConfig{Enabled: false}, 0},
		// GH-2432: nil RunTestsOnStop no longer installs the Stop hook by default.
		{"enabled with defaults", &HooksConfig{Enabled: true}, 1},               // PreToolUse only
		{"enabled with lint", &HooksConfig{Enabled: true, LintOnSave: true}, 2}, // PreToolUse + PostToolUse
		{"stop opt-in", &HooksConfig{Enabled: true, RunTestsOnStop: boolPtr(true)}, 2}, // Stop + PreToolUse
		{"all disabled", &HooksConfig{Enabled: true, RunTestsOnStop: boolPtr(false), BlockDestructive: boolPtr(false)}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateClaudeSettings(tt.config, "/test/scripts")

			if tt.expectKeys == 0 {
				if len(result) != 0 {
					t.Errorf("Expected empty result, got %d keys", len(result))
				}
				return
			}

			hooks, ok := result["hooks"].(map[string][]HookMatcherEntry)
			if !ok {
				t.Fatal("Expected hooks to be map[string][]HookMatcherEntry")
			}
			if len(hooks) != tt.expectKeys {
				t.Errorf("Expected %d hook types, got %d", tt.expectKeys, len(hooks))
			}
		})
	}
}

// TestGenerateClaudeSettingsJSONFormat verifies the JSON output matches Claude Code format:
// - PreToolUse/PostToolUse: "matcher" is a regex string
// - Stop: no "matcher" field
func TestGenerateClaudeSettingsJSONFormat(t *testing.T) {
	config := &HooksConfig{
		Enabled:        true,
		RunTestsOnStop: boolPtr(true), // GH-2432: Stop hook is now opt-in
		LintOnSave:     true,
	}

	settings := GenerateClaudeSettings(config, "/scripts")

	// Marshal to JSON to verify wire format
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal settings: %v", err)
	}

	// Unmarshal to generic map for format validation
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal settings: %v", err)
	}

	hooks := parsed["hooks"].(map[string]interface{})

	// Stop hook: must NOT have "matcher" field
	stopArr := hooks["Stop"].([]interface{})
	if len(stopArr) != 1 {
		t.Fatalf("Stop: expected 1 entry, got %d", len(stopArr))
	}
	stopEntry := stopArr[0].(map[string]interface{})
	if _, hasMatcher := stopEntry["matcher"]; hasMatcher {
		t.Error("Stop hook must NOT have matcher field")
	}
	stopHooks := stopEntry["hooks"].([]interface{})
	stopCmd := stopHooks[0].(map[string]interface{})
	if stopCmd["command"] != "/scripts/pilot-stop-gate.sh" {
		t.Errorf("Stop hook command: expected /scripts/pilot-stop-gate.sh, got %v", stopCmd["command"])
	}

	// PreToolUse hook: matcher must be a string "Bash"
	preArr := hooks["PreToolUse"].([]interface{})
	if len(preArr) != 1 {
		t.Fatalf("PreToolUse: expected 1 entry, got %d", len(preArr))
	}
	preEntry := preArr[0].(map[string]interface{})
	preMatcher, ok := preEntry["matcher"].(string)
	if !ok {
		t.Fatalf("PreToolUse matcher: expected string, got %T: %v", preEntry["matcher"], preEntry["matcher"])
	}
	if preMatcher != "Bash" {
		t.Errorf("PreToolUse matcher: expected 'Bash', got '%s'", preMatcher)
	}

	// PostToolUse hook: single entry with "Edit|Write" regex matcher
	postArr := hooks["PostToolUse"].([]interface{})
	if len(postArr) != 1 {
		t.Fatalf("PostToolUse: expected 1 entry, got %d", len(postArr))
	}
	postEntry := postArr[0].(map[string]interface{})
	postMatcher, ok := postEntry["matcher"].(string)
	if !ok {
		t.Fatalf("PostToolUse matcher: expected string, got %T: %v", postEntry["matcher"], postEntry["matcher"])
	}
	if postMatcher != "Edit|Write" {
		t.Errorf("PostToolUse matcher: expected 'Edit|Write', got '%s'", postMatcher)
	}
}

func TestWriteClaudeSettings(t *testing.T) {
	tempDir := t.TempDir()
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")

	bashMatcher := "Bash"
	settings := map[string]interface{}{
		"hooks": map[string][]HookMatcherEntry{
			"PreToolUse": {
				{
					Matcher: &bashMatcher,
					Hooks:   []HookCommand{{Type: "command", Command: "/test/script.sh"}},
				},
			},
			"Stop": {
				{
					// Matcher nil — Stop hooks must not have matcher
					Hooks: []HookCommand{{Type: "command", Command: "/test/stop.sh"}},
				},
			},
		},
	}

	err := WriteClaudeSettings(settingsPath, settings)
	if err != nil {
		t.Fatalf("Failed to write settings: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings file: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse written JSON: %v", err)
	}

	hooks := parsed["hooks"].(map[string]interface{})

	// Verify PreToolUse has string matcher
	preArr := hooks["PreToolUse"].([]interface{})
	preEntry := preArr[0].(map[string]interface{})
	if matcher, ok := preEntry["matcher"].(string); !ok || matcher != "Bash" {
		t.Errorf("PreToolUse matcher: expected string 'Bash', got %T %v", preEntry["matcher"], preEntry["matcher"])
	}

	// Verify Stop has no matcher
	stopArr := hooks["Stop"].([]interface{})
	stopEntry := stopArr[0].(map[string]interface{})
	if _, hasMatcher := stopEntry["matcher"]; hasMatcher {
		t.Error("Stop hook should not have matcher field")
	}
}

func TestWriteEmbeddedScripts(t *testing.T) {
	tempDir := t.TempDir()

	err := WriteEmbeddedScripts(tempDir)
	if err != nil {
		t.Fatalf("Failed to write embedded scripts: %v", err)
	}

	for _, script := range []string{"pilot-stop-gate.sh", "pilot-bash-guard.sh", "pilot-lint.sh"} {
		scriptPath := filepath.Join(tempDir, script)
		info, err := os.Stat(scriptPath)
		if err != nil {
			t.Errorf("Script %s not found: %v", script, err)
			continue
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("Script %s is not executable", script)
		}
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Errorf("Failed to read script %s: %v", script, err)
		}
		if len(content) == 0 {
			t.Errorf("Script %s is empty", script)
		}
	}
}

func TestGetBoolPtrValue(t *testing.T) {
	tests := []struct {
		name         string
		ptr          *bool
		defaultValue bool
		expected     bool
	}{
		{"nil ptr, default true", nil, true, true},
		{"nil ptr, default false", nil, false, false},
		{"false ptr, default true", boolPtr(false), true, false},
		{"true ptr, default false", boolPtr(true), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := GetBoolPtrValue(tt.ptr, tt.defaultValue); result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetScriptNames(t *testing.T) {
	tests := []struct {
		name     string
		config   *HooksConfig
		expected []string
	}{
		{"nil config", nil, nil},
		{"disabled", &HooksConfig{Enabled: false}, nil},
		// GH-2432: nil RunTestsOnStop no longer lists the Stop-hook script by default.
		{"defaults", &HooksConfig{Enabled: true}, []string{"pilot-bash-guard.sh"}},
		{"all features", &HooksConfig{Enabled: true, RunTestsOnStop: boolPtr(true), LintOnSave: true}, []string{"pilot-stop-gate.sh", "pilot-bash-guard.sh", "pilot-lint.sh"}},
		{"all disabled", &HooksConfig{Enabled: true, RunTestsOnStop: boolPtr(false), BlockDestructive: boolPtr(false)}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetScriptNames(tt.config)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d scripts, got %d", len(tt.expected), len(result))
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

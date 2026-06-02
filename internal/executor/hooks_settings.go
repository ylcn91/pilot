package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateClaudeSettings builds the .claude/settings.json structure with hook entries
// Uses Claude Code 2.1.42+ matcher-based array format
func GenerateClaudeSettings(config *HooksConfig, scriptDir string) map[string]interface{} {
	if config == nil || !config.Enabled {
		return map[string]interface{}{}
	}

	hooks := make(map[string][]HookMatcherEntry)

	// Stop hook: run tests before Claude finishes (no matcher — Stop hooks must omit it)
	if config.RunTestsOnStop == nil || *config.RunTestsOnStop {
		hooks["Stop"] = []HookMatcherEntry{
			{
				// Matcher intentionally nil — Stop hooks must not have matcher field
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-stop-gate.sh"),
					},
				},
			},
		}
	}

	// PreToolUse hook: block destructive Bash commands (matcher is regex string)
	if config.BlockDestructive == nil || *config.BlockDestructive {
		hooks["PreToolUse"] = []HookMatcherEntry{
			{
				Matcher: stringPtr("Bash"),
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-bash-guard.sh"),
					},
				},
			},
		}
	}

	// PostToolUse hook: lint files after changes (opt-in, single entry with regex matcher)
	if config.LintOnSave {
		hooks["PostToolUse"] = []HookMatcherEntry{
			{
				Matcher: stringPtr("Edit|Write"),
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-lint.sh"),
					},
				},
			},
		}
	}

	if len(hooks) == 0 {
		return map[string]interface{}{}
	}

	return map[string]interface{}{
		"hooks": hooks,
	}
}

// WriteClaudeSettings writes the .claude/settings.json file
func WriteClaudeSettings(settingsPath string, settings map[string]interface{}) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Write settings as JSON
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}

	return nil
}

// WriteEmbeddedScripts extracts embedded hook scripts to the specified directory
func WriteEmbeddedScripts(scriptDir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return fmt.Errorf("failed to create script directory: %w", err)
	}

	// Read embedded scripts
	entries, err := embeddedHookScripts.ReadDir("hookscripts")
	if err != nil {
		return fmt.Errorf("failed to read embedded scripts: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Read script content
		content, err := embeddedHookScripts.ReadFile(filepath.Join("hookscripts", entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded script %s: %w", entry.Name(), err)
		}

		// Write to target directory with executable permissions
		scriptPath := filepath.Join(scriptDir, entry.Name())
		if err := os.WriteFile(scriptPath, content, 0755); err != nil {
			return fmt.Errorf("failed to write script %s: %w", entry.Name(), err)
		}
	}

	return nil
}

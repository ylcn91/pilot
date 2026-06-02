package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MergeWithExisting merges Pilot hooks with existing .claude/settings.json
// Returns a restore function to revert changes and any error
// Handles both old format (map[string]HookDefinition) and new format (map[string][]HookMatcherEntry)
func MergeWithExisting(settingsPath string, pilotSettings map[string]interface{}) (restoreFunc func() error, err error) {
	var originalData []byte
	var originalExists bool

	// Read existing settings if they exist
	if data, readErr := os.ReadFile(settingsPath); readErr == nil {
		originalData = data
		originalExists = true
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("failed to read existing settings: %w", readErr)
	}

	// If no Pilot hooks to add, no-op
	if len(pilotSettings) == 0 {
		return func() error { return nil }, nil
	}

	var merged map[string]interface{}

	if originalExists && len(originalData) > 0 {
		// Parse existing settings
		var existing map[string]interface{}
		if err := json.Unmarshal(originalData, &existing); err != nil {
			return nil, fmt.Errorf("failed to parse existing settings: %w", err)
		}

		// Deep merge hooks section
		merged = make(map[string]interface{})
		for k, v := range existing {
			merged[k] = v
		}

		if pilotHooks, ok := pilotSettings["hooks"]; ok {
			if existingHooks, exists := merged["hooks"]; exists {
				// Check if existing hooks are in old format (object with command) or new format (arrays)
				if existingHooksMap, ok := existingHooks.(map[string]interface{}); ok {
					if isOldHookFormat(existingHooksMap) {
						// Old format detected - don't corrupt it, just add our new format hooks
						// Keep existing as-is and append our hooks
						merged["hooks"] = pilotHooks
					} else {
						// New format - merge arrays by hook event type
						mergedHooks := mergeNewFormatHooks(existingHooksMap, pilotHooks)
						merged["hooks"] = mergedHooks
					}
				} else {
					// Unknown format, replace with pilot hooks
					merged["hooks"] = pilotHooks
				}
			} else {
				// No existing hooks, add pilot hooks
				merged["hooks"] = pilotHooks
			}
		}
	} else {
		// No existing settings, use pilot settings directly
		merged = pilotSettings
	}

	// Write merged settings
	if err := WriteClaudeSettings(settingsPath, merged); err != nil {
		return nil, fmt.Errorf("failed to write merged settings: %w", err)
	}

	// Return restore function
	restoreFunc = func() error {
		if originalExists {
			// Restore original file
			return os.WriteFile(settingsPath, originalData, 0644)
		} else {
			// Remove file we created
			if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove settings file: %w", err)
			}
		}
		return nil
	}

	return restoreFunc, nil
}

// isOldHookFormat checks if the hooks map is in old format (e.g., "Stop": {"command": "..."})
// Old format has string keys with object values containing "command" field
// New format has string keys with array values
func isOldHookFormat(hooks map[string]interface{}) bool {
	for _, v := range hooks {
		// In old format, each value is an object with "command" field
		if obj, ok := v.(map[string]interface{}); ok {
			if _, hasCommand := obj["command"]; hasCommand {
				return true
			}
		}
		// In new format, each value is an array
		if _, isArray := v.([]interface{}); isArray {
			return false
		}
	}
	return false
}

// mergeNewFormatHooks merges pilot hooks into existing hooks, deduplicating by command path.
// Also cleans stale entries whose command paths no longer exist on disk.
func mergeNewFormatHooks(existing map[string]interface{}, pilot interface{}) map[string]interface{} {
	mergedHooks := make(map[string]interface{})

	// Start with pilot hooks (fresh, authoritative)
	switch ph := pilot.(type) {
	case map[string][]HookMatcherEntry:
		for k, v := range ph {
			mergedHooks[k] = v
		}
	case map[string]interface{}:
		for k, v := range ph {
			mergedHooks[k] = v
		}
	}

	// Collect pilot command paths for dedup
	pilotCommands := make(map[string]bool)
	for _, v := range mergedHooks {
		extractHookCommands(v, pilotCommands)
	}

	// Merge non-pilot existing entries (skip duplicates and stale entries)
	for k, v := range existing {
		if _, hasPilot := mergedHooks[k]; !hasPilot {
			// Hook type not in pilot config — keep existing entries that are still valid
			if cleaned := cleanStaleEntries(v); cleaned != nil {
				mergedHooks[k] = cleaned
			}
			continue
		}
		// Hook type exists in both — preserve non-pilot, non-stale entries
		if existingArr, ok := v.([]interface{}); ok {
			for _, entry := range existingArr {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					cmd := extractCommandFromEntry(entryMap)
					if cmd != "" && !pilotCommands[cmd] && !isPilotManagedHook(cmd) && hookFileExists(cmd) {
						mergedHooks[k] = appendHookEntry(mergedHooks[k], entry)
					}
				}
			}
		}
	}

	return mergedHooks
}

// extractHookCommands collects all command paths from a hook value
func extractHookCommands(v interface{}, commands map[string]bool) {
	switch val := v.(type) {
	case []HookMatcherEntry:
		for _, entry := range val {
			for _, h := range entry.Hooks {
				commands[h.Command] = true
			}
		}
	case []interface{}:
		for _, entry := range val {
			if m, ok := entry.(map[string]interface{}); ok {
				if cmd := extractCommandFromEntry(m); cmd != "" {
					commands[cmd] = true
				}
			}
		}
	}
}

// extractCommandFromEntry gets the command path from a hook entry map
func extractCommandFromEntry(entry map[string]interface{}) string {
	if hooks, ok := entry["hooks"].([]interface{}); ok {
		for _, h := range hooks {
			if hm, ok := h.(map[string]interface{}); ok {
				if cmd, ok := hm["command"].(string); ok {
					return cmd
				}
			}
		}
	}
	return ""
}

// cleanStaleEntries removes entries whose command files no longer exist
// or are stale pilot-managed hooks from previous runs
func cleanStaleEntries(v interface{}) interface{} {
	arr, ok := v.([]interface{})
	if !ok {
		return v
	}
	var cleaned []interface{}
	for _, entry := range arr {
		if m, ok := entry.(map[string]interface{}); ok {
			cmd := extractCommandFromEntry(m)
			// Keep entry if: no command (unknown format), or file exists and not a stale pilot hook
			if cmd == "" || (hookFileExists(cmd) && !isPilotManagedHook(cmd)) {
				cleaned = append(cleaned, entry)
			}
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// appendHookEntry appends an entry to a hook value (handles both typed and untyped slices)
func appendHookEntry(existing interface{}, entry interface{}) interface{} {
	if arr, ok := existing.([]interface{}); ok {
		return append(arr, entry)
	}
	if arr, ok := existing.([]HookMatcherEntry); ok {
		result := make([]interface{}, len(arr))
		for i, e := range arr {
			result[i] = e
		}
		return append(result, entry)
	}
	return existing
}

// hookFileExists checks if a hook script file exists on disk
func hookFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isPilotManagedHook checks if a command path is a pilot-generated hook script.
// Pilot scripts are always named pilot-*.sh (e.g., pilot-bash-guard.sh, pilot-stop-gate.sh).
// Used during merge to prevent accumulation of stale pilot entries from different temp dirs.
func isPilotManagedHook(cmd string) bool {
	base := filepath.Base(cmd)
	return strings.HasPrefix(base, "pilot-") && strings.HasSuffix(base, ".sh")
}

// CleanStalePilotHooks removes stale pilot hook entries from .claude/settings.json.
// Called on startup regardless of hooks.enabled to prevent accumulation of dead entries
// from previous runs (e.g. after OS reboot clears temp dirs, or after format upgrades).
//
// Removes entries where:
//   - isPilotManagedHook(cmd) is true AND hookFileExists(cmd) is false (dead script paths)
//   - OR the entry has an old object-format matcher ({"tools": [...]}) instead of a string
//
// Non-pilot user hooks are always preserved.
func CleanStalePilotHooks(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read settings: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse settings: %w", err)
	}

	hooksRaw, ok := raw["hooks"]
	if !ok {
		return nil
	}

	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	cleaned := make(map[string]interface{})
	totalBefore, totalAfter := 0, 0

	for event, entries := range hooksMap {
		arr, ok := entries.([]interface{})
		if !ok {
			cleaned[event] = entries
			continue
		}

		totalBefore += len(arr)

		var kept []interface{}
		for _, entry := range arr {
			m, ok := entry.(map[string]interface{})
			if !ok {
				kept = append(kept, entry)
				continue
			}

			// Remove entries with old object-format matcher: {"tools": [...]}
			if matcherVal, hasMatcher := m["matcher"]; hasMatcher {
				if _, isString := matcherVal.(string); !isString {
					continue
				}
			}

			// Remove stale pilot hooks whose script files are gone
			cmd := extractCommandFromEntry(m)
			if cmd != "" && isPilotManagedHook(cmd) && !hookFileExists(cmd) {
				continue
			}

			kept = append(kept, entry)
		}

		totalAfter += len(kept)
		if len(kept) > 0 {
			cleaned[event] = kept
		}
	}

	if totalBefore == totalAfter {
		return nil
	}

	if len(cleaned) == 0 {
		delete(raw, "hooks")
	} else {
		raw["hooks"] = cleaned
	}

	return WriteClaudeSettings(settingsPath, raw)
}

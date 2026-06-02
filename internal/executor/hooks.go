package executor

import (
	"embed"
)

//go:embed hookscripts/*
var embeddedHookScripts embed.FS

// HooksConfig configures Claude Code hooks for quality gates during execution.
// Hooks run inline during Claude execution instead of after completion,
// catching issues while context is still available.
//
// Example YAML configuration:
//
//	executor:
//	  hooks:
//	    enabled: true
//	    run_tests_on_stop: true    # Stop hook runs tests (default when enabled)
//	    block_destructive: true    # PreToolUse hook blocks dangerous commands (default when enabled)
//	    lint_on_save: false       # PostToolUse hook runs linter after file changes
type HooksConfig struct {
	// Enabled controls whether Claude Code hooks are active.
	// When false (default), hooks are not installed and execution proceeds normally.
	Enabled bool `yaml:"enabled"`

	// RunTestsOnStop enables the Stop hook that runs build/tests before Claude finishes.
	// When enabled, Claude must fix any build/test failures before completing.
	// Default: true when Enabled is true
	RunTestsOnStop *bool `yaml:"run_tests_on_stop,omitempty"`

	// BlockDestructive enables the PreToolUse hook that blocks dangerous Bash commands.
	// Prevents commands like "rm -rf /", "git push --force", "DROP TABLE", "git reset --hard".
	// Default: true when Enabled is true
	BlockDestructive *bool `yaml:"block_destructive,omitempty"`

	// LintOnSave enables the PostToolUse hook that runs linter after Edit/Write tools.
	// Automatically formats/lints files after changes.
	// Default: false (opt-in feature)
	LintOnSave bool `yaml:"lint_on_save,omitempty"`
}

// DefaultHooksConfig returns default hooks configuration.
// GH-2432: RunTestsOnStop default flipped to false. Stop-hook tests forced
// long unproductive turns into the Claude session, inflating token cost
// without comparable quality gain. Quality gates still run after the
// subprocess exits.
func DefaultHooksConfig() *HooksConfig {
	runTestsOnStop := false
	blockDestructive := true
	return &HooksConfig{
		Enabled:          false, // Disabled by default, opt-in feature
		RunTestsOnStop:   &runTestsOnStop,
		BlockDestructive: &blockDestructive,
		LintOnSave:       false,
	}
}

// ClaudeSettings represents the structure of .claude/settings.json
// Uses Claude Code 2.1.42+ matcher-based hook format
type ClaudeSettings struct {
	Hooks map[string][]HookMatcherEntry `json:"hooks,omitempty"`
}

// HookMatcherEntry defines a matcher-based hook entry (Claude Code 2.1.42+)
// For PreToolUse/PostToolUse: matcher is a regex string (e.g. "Bash", "Edit|Write")
// For Stop: matcher field must be omitted entirely
type HookMatcherEntry struct {
	Matcher *string       `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// stringPtr returns a pointer to a string (helper for HookMatcherEntry.Matcher)
func stringPtr(s string) *string { return &s }

// HookMatcher is kept for backward compatibility with old format parsing.
// New code should use *string matcher in HookMatcherEntry.
type HookMatcher struct {
	Tools []string `json:"tools,omitempty"`
}

// HookCommand defines a single hook command
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookDefinition is kept for backward compatibility with old settings format
type HookDefinition struct {
	Command string `json:"command"`
}

// GetBoolPtrValue returns the value of a bool pointer or the default value if nil
func GetBoolPtrValue(ptr *bool, defaultValue bool) bool {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// GetScriptNames returns the list of required script names for validation
func GetScriptNames(config *HooksConfig) []string {
	if config == nil || !config.Enabled {
		return nil
	}

	var scripts []string

	if GetBoolPtrValue(config.RunTestsOnStop, true) {
		scripts = append(scripts, "pilot-stop-gate.sh")
	}

	if GetBoolPtrValue(config.BlockDestructive, true) {
		scripts = append(scripts, "pilot-bash-guard.sh")
	}

	if config.LintOnSave {
		scripts = append(scripts, "pilot-lint.sh")
	}

	return scripts
}

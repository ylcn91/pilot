package executor

import "fmt"

// TDDConfig configures an opt-in test-driven-development run mode. When nil or
// Enabled=false, execution is byte-identical to today's single-pass behavior.
//
// When enabled, a run is decomposed into four sequential roles, each optionally
// backed by a different runnable backend (defaulting to the run's primary
// backend when its StageConfig is nil):
//
//	ARCHITECT    (read-only design)
//	TEST-AUTHOR  (writes FAILING tests, commits) -> RED gate: tests MUST fail
//	IMPLEMENTER  (makes them pass, commits)       -> GREEN gate: same tests pass
//	QA           (the existing finalize tail: copy result + finalize)
//
// Each role reuses StageConfig + NewStageBackend, exactly like PipelineConfig.
type TDDConfig struct {
	// Enabled gates the entire TDD mode. False/absent => no behavior change.
	Enabled bool `yaml:"enabled,omitempty"`

	// Architect drives the read-only design phase. Nil => primary backend.
	Architect *StageConfig `yaml:"architect,omitempty"`

	// TestAuthor writes the failing tests. Nil => primary backend.
	TestAuthor *StageConfig `yaml:"test_author,omitempty"`

	// Implementer makes the failing tests pass. Nil => primary backend.
	Implementer *StageConfig `yaml:"implementer,omitempty"`

	// QA drives the finalize tail (review/PR). Nil => primary backend.
	QA *StageConfig `yaml:"qa,omitempty"`

	// GreenMaxRetries bounds IMPLEMENTER retries on a failing GREEN gate.
	// Nil => default 2.
	GreenMaxRetries *int `yaml:"green_max_retries,omitempty"`

	// RoleMaxRetries bounds per-role retries (e.g. one TEST-AUTHOR retry before
	// the RED gate must be red). Nil => default 1.
	RoleMaxRetries *int `yaml:"role_max_retries,omitempty"`

	// ScopeRedToNew scopes the RED/GREEN gate to only the newly authored test
	// names (e.g. `go test -run '^(TestX|TestY)$' ./...`) instead of the whole
	// suite. Nil => default true.
	ScopeRedToNew *bool `yaml:"scope_red_to_new,omitempty"`
}

// Validate checks each present role targets a runnable backend, mirroring
// (*PipelineConfig).Validate. A nil or disabled config is valid. codex-app-server
// is rejected (it is not a runnable Backend); use codex-exec instead.
func (t *TDDConfig) Validate() error {
	if t == nil || !t.Enabled {
		return nil
	}
	roles := []struct {
		name  string
		stage *StageConfig
	}{
		{"architect", t.Architect},
		{"test_author", t.TestAuthor},
		{"implementer", t.Implementer},
		{"qa", t.QA},
	}
	for _, r := range roles {
		if r.stage == nil {
			continue
		}
		if r.stage.Type == "" {
			return fmt.Errorf("tdd.%s.type is required when the role is present", r.name)
		}
		if r.stage.Type == backendTypeCodexAppServer {
			return fmt.Errorf("tdd.%s.type %q is not a runnable Backend; use codex-exec for a TDD role", r.name, r.stage.Type)
		}
		if !runnablePipelineBackends[r.stage.Type] {
			return fmt.Errorf("tdd.%s.type %q is not a known backend (use one of: claude-code, codex-exec, qwen-code, anthropic-api, openai-api, opencode)", r.name, r.stage.Type)
		}
	}
	return nil
}

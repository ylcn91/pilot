package architect

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

func TestProposeCodexExecStageIsRunnableForRefactorSlant(t *testing.T) {
	stage := &executor.StageConfig{Type: executor.BackendTypeCodexExec}
	base := executor.BackendConfig{Type: executor.BackendTypeClaudeCode}
	mb := &mockBackend{output: `[{"title":"Split scanner","kind":"refactor","risk":"low"}]`}
	a := NewAnalyzer(stage, base, "", WithSlant(refactorSlant))

	var gotStage *executor.StageConfig
	var gotBase executor.BackendConfig
	a.newBackend = func(s *executor.StageConfig, b executor.BackendConfig) (executor.Backend, error) {
		gotStage = s
		gotBase = b
		return mb, nil
	}

	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose with codex-exec stage: %v", err)
	}
	if gotStage == nil || gotStage.Type != executor.BackendTypeCodexExec {
		t.Fatalf("stage = %+v, want codex-exec", gotStage)
	}
	if gotBase.Type != executor.BackendTypeClaudeCode {
		t.Fatalf("base backend = %q, want claude-code", gotBase.Type)
	}
	if mb.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", mb.calls)
	}
}

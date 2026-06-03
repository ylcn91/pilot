package config

import (
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

func TestArchitectConfig_AcceptsCodexExecBackend(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled: true,
		Backend: &executor.StageConfig{Type: executor.BackendTypeCodexExec},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("codex-exec architect backend must validate: %v", err)
	}
}

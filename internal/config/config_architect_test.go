package config

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
)

func TestArchitectConfig_NilIsValid(t *testing.T) {
	c := baseValidConfig()
	c.Architect = nil
	if err := c.Validate(); err != nil {
		t.Fatalf("nil architect must be valid: %v", err)
	}
}

func TestArchitectConfig_DisabledSkipsAllChecks(t *testing.T) {
	c := baseValidConfig()
	// Deliberately invalid fields, but disabled => Validate must not look.
	c.Architect = &ArchitectConfig{
		Enabled:    false,
		Schedule:   "not a cron",
		MaxTickets: -5,
		Backend:    &executor.StageConfig{Type: "codex-app-server"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("disabled architect must skip validation: %v", err)
	}
}

func TestArchitectConfig_ValidBlockAccepted(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled:    true,
		Schedule:   "0 8 * * 1",
		Timezone:   "America/New_York",
		Backend:    &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
		MaxTickets: 10,
		Labels:     []string{"pilot", "architect"},
		Thresholds: ArchitectThresholds{LOC: 400, MinCoverage: 80},
		Signals:    []string{"loc_over_400", "todo_fixme"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid architect block rejected: %v", err)
	}
}

func TestArchitectConfig_EnabledNilBackendValid(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{Enabled: true}
	if err := c.Validate(); err != nil {
		t.Fatalf("enabled architect with nil backend must be valid: %v", err)
	}
}

func TestArchitectConfig_CodexExecBackendAccepted(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled: true,
		Backend: &executor.StageConfig{Type: executor.BackendTypeCodexExec},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("codex-exec must be valid for architect backend: %v", err)
	}
}

func TestArchitectConfig_RejectsCodexAppServerBackend(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled: true,
		Backend: &executor.StageConfig{Type: "codex-app-server"},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("codex-app-server backend must be rejected")
	}
	if !strings.Contains(err.Error(), "architect.backend") {
		t.Errorf("error should be scoped to architect.backend, got %q", err)
	}
	if !strings.Contains(err.Error(), "not a runnable Backend") || !strings.Contains(err.Error(), "use codex-exec") {
		t.Errorf("error should explain runnable backend path, got %q", err)
	}
}

func TestArchitectConfig_RejectsUnknownBackend(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled: true,
		Backend: &executor.StageConfig{Type: "totally-made-up"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("unknown backend type must be rejected")
	}
}

func TestArchitectConfig_RejectsBackendMissingType(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled: true,
		Backend: &executor.StageConfig{}, // present but no Type
	}
	if err := c.Validate(); err == nil {
		t.Fatal("a present backend with empty type must be rejected")
	}
}

func TestArchitectConfig_RejectsNegativeMaxTickets(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{Enabled: true, MaxTickets: -1}
	err := c.Validate()
	if err == nil {
		t.Fatal("MaxTickets < 1 must be rejected")
	}
	if !strings.Contains(err.Error(), "max_tickets") {
		t.Errorf("error should mention max_tickets, got %q", err)
	}
}

func TestArchitectConfig_ZeroMaxTicketsIsValid(t *testing.T) {
	c := baseValidConfig()
	// Zero means "use the default at use-site" — not an error.
	c.Architect = &ArchitectConfig{Enabled: true, MaxTickets: 0}
	if err := c.Validate(); err != nil {
		t.Fatalf("MaxTickets 0 (default sentinel) must be valid: %v", err)
	}
}

func TestArchitectConfig_RejectsBadCron(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{Enabled: true, Schedule: "every blue moon"}
	err := c.Validate()
	if err == nil {
		t.Fatal("invalid cron schedule must be rejected")
	}
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("error should mention schedule, got %q", err)
	}
}

func TestArchitectConfig_EmptyScheduleIsValid(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{Enabled: true, Schedule: ""}
	if err := c.Validate(); err != nil {
		t.Fatalf("empty schedule (on-demand only) must be valid: %v", err)
	}
}

func TestArchitectConfig_NilReceiverValidate(t *testing.T) {
	var a *ArchitectConfig
	if err := a.Validate(); err != nil {
		t.Fatalf("nil receiver Validate must be a no-op: %v", err)
	}
}

func TestArchitectConfig_EmptyExportIsValid(t *testing.T) {
	c := baseValidConfig()
	// Empty export falls back to the GitHub default at use-site — not an error.
	c.Architect = &ArchitectConfig{Enabled: true, Export: ""}
	if err := c.Validate(); err != nil {
		t.Fatalf("empty export (default sentinel) must be valid: %v", err)
	}
}

func TestArchitectConfig_ValidExportsAccepted(t *testing.T) {
	for _, target := range ValidArchitectExports {
		t.Run(target, func(t *testing.T) {
			c := baseValidConfig()
			c.Architect = &ArchitectConfig{Enabled: true, Export: target}
			if err := c.Validate(); err != nil {
				t.Fatalf("export %q must be valid: %v", target, err)
			}
		})
	}
}

func TestArchitectConfig_RejectsUnknownExport(t *testing.T) {
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{Enabled: true, Export: "jira-but-not-yet"}
	err := c.Validate()
	if err == nil {
		t.Fatal("an unknown export target must be rejected")
	}
	if !strings.Contains(err.Error(), "export") {
		t.Errorf("error should mention export, got %q", err)
	}
}

func TestArchitectConfig_RejectsBackendThenStillReportsCronWhenBackendOK(t *testing.T) {
	// Worst case: a valid backend but a bad cron must still fail, proving the
	// cron check runs after the backend check rather than being short-circuited.
	c := baseValidConfig()
	c.Architect = &ArchitectConfig{
		Enabled:  true,
		Backend:  &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
		Schedule: "@@@",
	}
	if err := c.Validate(); err == nil {
		t.Fatal("bad cron with a good backend must still fail")
	}
}

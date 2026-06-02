package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
)

// ---------------------------------------------------------------------------
// checkAgentDocSize
// ---------------------------------------------------------------------------

func TestCheckAgentDocSize_CleanDir(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, ".agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a small file that should not trigger any check.
	if err := os.WriteFile(filepath.Join(agentDir, "small.md"), []byte(makeLines(100)), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := checkAgentDocSize(agentDir)
	if len(checks) != 0 {
		t.Errorf("expected 0 checks for clean dir, got %d: %+v", len(checks), checks)
	}
}

func TestCheckAgentDocSize_WarnFile(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, ".agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 600 lines — above warn threshold (500) but below fail threshold (1000).
	if err := os.WriteFile(filepath.Join(agentDir, "big.md"), []byte(makeLines(600)), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := checkAgentDocSize(agentDir)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != StatusWarning {
		t.Errorf("expected StatusWarning, got %v", checks[0].Status)
	}
}

func TestCheckAgentDocSize_FailFile(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, ".agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 1100 lines — above fail threshold (1000).
	if err := os.WriteFile(filepath.Join(agentDir, "bloated.md"), []byte(makeLines(1100)), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := checkAgentDocSize(agentDir)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != StatusError {
		t.Errorf("expected StatusError, got %v", checks[0].Status)
	}
}

func TestCheckAgentDocSize_MissingDir(t *testing.T) {
	checks := checkAgentDocSize("/nonexistent/path/.agent")
	if len(checks) != 0 {
		t.Errorf("expected 0 checks for missing dir, got %d", len(checks))
	}
}

func TestCheckAgentDocSize_SubdirFile(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, ".agent")
	subDir := filepath.Join(agentDir, "sops")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 1100-line file in a subdirectory.
	if err := os.WriteFile(filepath.Join(subDir, "deep.md"), []byte(makeLines(1100)), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := checkAgentDocSize(agentDir)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != StatusError {
		t.Errorf("expected StatusError, got %v", checks[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Approval misconfig doctor check
// ---------------------------------------------------------------------------

func TestCheckConfig_ApprovalMisconfig_Detected(t *testing.T) {
	// env has require_approval=true but approval.pre_merge is disabled → StatusError
	cfg := config.DefaultConfig()
	cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
	cfg.Orchestrator.Autopilot.Environments = map[string]*autopilot.EnvironmentConfig{
		"stage": {RequireApproval: true},
	}
	approvalCfg := approval.DefaultConfig()
	approvalCfg.Enabled = false
	cfg.Approval = approvalCfg

	checks := checkConfig(cfg)
	found := false
	for _, c := range checks {
		if c.Name == "approval-misconfig" {
			found = true
			if c.Status != StatusError {
				t.Errorf("approval-misconfig status = %v, want StatusError", c.Status)
			}
			if !strings.Contains(c.Message, "stage") {
				t.Errorf("approval-misconfig message should mention env name, got: %s", c.Message)
			}
		}
	}
	if !found {
		t.Error("expected approval-misconfig check to appear in ConfigChecks")
	}
}

func TestCheckConfig_ApprovalMisconfig_NotReported_WhenPreMergeEnabled(t *testing.T) {
	// env has require_approval=true AND approval.pre_merge is enabled → no misconfig check
	cfg := config.DefaultConfig()
	cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
	cfg.Orchestrator.Autopilot.Environments = map[string]*autopilot.EnvironmentConfig{
		"stage": {RequireApproval: true},
	}
	approvalCfg := approval.DefaultConfig()
	approvalCfg.Enabled = true
	approvalCfg.PreMerge = &approval.StageConfig{Enabled: true}
	cfg.Approval = approvalCfg

	checks := checkConfig(cfg)
	for _, c := range checks {
		if c.Name == "approval-misconfig" {
			t.Errorf("unexpected approval-misconfig check when pre_merge is enabled: %+v", c)
		}
	}
}

func TestCheckConfig_ApprovalMisconfig_NotReported_WhenNoEnvRequiresApproval(t *testing.T) {
	// No env requires approval → no misconfig check even if approval is disabled
	cfg := config.DefaultConfig()
	cfg.Orchestrator.Autopilot = autopilot.DefaultConfig()
	cfg.Orchestrator.Autopilot.Environments = map[string]*autopilot.EnvironmentConfig{
		"stage": {RequireApproval: false},
	}
	approvalCfg := approval.DefaultConfig()
	approvalCfg.Enabled = false
	cfg.Approval = approvalCfg

	checks := checkConfig(cfg)
	for _, c := range checks {
		if c.Name == "approval-misconfig" {
			t.Errorf("unexpected approval-misconfig check when no env requires approval: %+v", c)
		}
	}
}

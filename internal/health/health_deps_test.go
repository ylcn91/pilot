package health

import (
	"testing"
)

// ---------------------------------------------------------------------------
// checkDependencies — integration test (runs on dev machine)
// ---------------------------------------------------------------------------

func TestCheckDependencies_ReturnsChecks(t *testing.T) {
	deps := checkDependencies()

	// Should have at minimum claude, git, gh
	if len(deps) < 3 {
		t.Errorf("expected at least 3 dependency checks, got %d", len(deps))
	}

	// git should be available on any dev machine
	gitCheck := findCheck(deps, "git")
	if gitCheck == nil {
		t.Fatal("expected 'git' dependency check")
	}
	if gitCheck.Status != StatusOK {
		t.Errorf("git status = %v, want StatusOK (is git installed?)", gitCheck.Status)
	}
}

// ---------------------------------------------------------------------------
// getCommandVersion
// ---------------------------------------------------------------------------

func TestGetCommandVersion_ValidCommand(t *testing.T) {
	// 'git --version' should work everywhere
	version := getCommandVersion("git", "--version")
	if version == "" {
		t.Skip("git not installed, skipping")
	}
	// Should contain a dot (version number like "2.39.0")
	if len(version) < 3 {
		t.Errorf("getCommandVersion(git) = %q, expected version string", version)
	}
}

func TestGetCommandVersion_InvalidCommand(t *testing.T) {
	version := getCommandVersion("nonexistent_command_xyz", "--version")
	if version != "" {
		t.Errorf("expected empty string for nonexistent command, got %q", version)
	}
}

// ---------------------------------------------------------------------------
// commandExists
// ---------------------------------------------------------------------------

func TestCommandExists(t *testing.T) {
	if !commandExists("git") {
		t.Skip("git not installed, skipping")
	}
	if commandExists("nonexistent_command_xyz_123") {
		t.Error("expected false for nonexistent command")
	}
}

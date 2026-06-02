// Package health provides system health checks for Pilot.
//
// It verifies required dependencies (Claude Code CLI, git) are installed
// and checks feature availability based on configuration. The RunChecks function
// generates a HealthReport used by the CLI status command to display system
// readiness and configuration state.
package health

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/config"
)

// Status represents feature or dependency status
type Status int

const (
	StatusOK Status = iota
	StatusWarning
	StatusError
	StatusDisabled
)

// Check represents a health check result
type Check struct {
	Name    string
	Status  Status
	Message string
	Fix     string
}

// ConfigCheck represents a configuration check result
type ConfigCheck struct {
	Name    string
	Status  Status
	Message string
	Fix     string
}

// FeatureStatus represents a feature with its availability
type FeatureStatus struct {
	Name     string
	Enabled  bool
	Status   Status
	Note     string
	Missing  []string // What's missing to enable this feature
	Degraded bool     // Feature works but with reduced functionality
}

// HealthReport contains all health check results
type HealthReport struct {
	Dependencies []Check
	Config       []ConfigCheck
	Features     []FeatureStatus
	Projects     int
	HasErrors    bool
	HasWarnings  bool
}

// RunChecks performs all health checks based on config
func RunChecks(cfg *config.Config) *HealthReport {
	// Determine active backend type from config
	backendType := "codex-exec" // default
	if cfg.Executor != nil && cfg.Executor.Type != "" {
		backendType = cfg.Executor.Type
	}

	configChecks := checkConfig(cfg)
	if cwd, err := os.Getwd(); err == nil {
		configChecks = append(configChecks, checkAgentDocSize(filepath.Join(cwd, ".agent"))...)
	}
	configChecks = append(configChecks, checkBrewTapHealth(brewTapHTTPGet))

	report := &HealthReport{
		Dependencies: checkDependenciesWithBackend(backendType),
		Config:       configChecks,
		Features:     checkFeatures(cfg),
		Projects:     len(cfg.Projects),
	}

	// Check for errors/warnings
	for _, d := range report.Dependencies {
		if d.Status == StatusError {
			report.HasErrors = true
		}
		if d.Status == StatusWarning {
			report.HasWarnings = true
		}
	}
	for _, c := range report.Config {
		if c.Status == StatusError {
			report.HasErrors = true
		}
		if c.Status == StatusWarning {
			report.HasWarnings = true
		}
	}
	for _, f := range report.Features {
		if f.Status == StatusError {
			report.HasErrors = true
		}
		if f.Status == StatusWarning || f.Degraded {
			report.HasWarnings = true
		}
	}

	return report
}

// getCommandVersion runs a command and returns its version string
func getCommandVersion(cmd string, args ...string) string {
	out, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(out))
	// Extract just version number if possible
	if strings.Contains(version, " ") {
		parts := strings.Fields(version)
		for _, p := range parts {
			if strings.Contains(p, ".") {
				return p
			}
		}
	}
	return version
}

// commandExists checks if a command exists in PATH
func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

// boolToStatus converts bool to Status
func boolToStatus(enabled bool) Status {
	if enabled {
		return StatusOK
	}
	return StatusDisabled
}

// Symbol returns the symbol for a status
func (s Status) Symbol() string {
	switch s {
	case StatusOK:
		return "✓"
	case StatusWarning:
		return "○"
	case StatusError:
		return "✗"
	case StatusDisabled:
		return "·"
	default:
		return "?"
	}
}

// ColorSymbol returns the colored symbol for a status
func (s Status) ColorSymbol() string {
	switch s {
	case StatusOK:
		return "\033[32m✓\033[0m" // green
	case StatusWarning:
		return "\033[33m○\033[0m" // yellow
	case StatusError:
		return "\033[31m✗\033[0m" // red
	case StatusDisabled:
		return "\033[90m·\033[0m" // gray
	default:
		return "?"
	}
}

// String returns string representation
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarning:
		return "warning"
	case StatusError:
		return "error"
	case StatusDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// Summary returns a summary of issues
func (r *HealthReport) Summary() (errors int, warnings int) {
	for _, d := range r.Dependencies {
		if d.Status == StatusError {
			errors++
		}
		if d.Status == StatusWarning {
			warnings++
		}
	}
	for _, c := range r.Config {
		if c.Status == StatusError {
			errors++
		}
		if c.Status == StatusWarning {
			warnings++
		}
	}
	return
}

// ReadyToStart returns true if there are no critical errors
func (r *HealthReport) ReadyToStart() bool {
	// Check for critical dependency errors
	for _, d := range r.Dependencies {
		// git is always required
		if d.Name == "git" && d.Status == StatusError {
			return false
		}
		// Any backend marked as active (contains "[active backend]") that's missing is critical
		if d.Status == StatusError && strings.Contains(d.Message, "[active backend]") {
			return false
		}
	}
	return true
}

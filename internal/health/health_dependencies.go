package health

import (
	"os/exec"
	"runtime"
	"strings"
)

// backendInfo holds metadata about a backend for health checks
type backendInfo struct {
	name        string   // display name (e.g., "claude")
	backendType string   // executor.BackendType constant (e.g., "claude-code")
	command     string   // CLI command to check
	versionArgs []string // args to get version (e.g., ["--version"])
	installCmd  string   // install instruction
}

var backends = []backendInfo{
	{
		name:        "claude",
		backendType: "claude-code",
		command:     "claude",
		versionArgs: []string{"--version"},
		installCmd:  "npm install -g @anthropic-ai/claude-code",
	},
	{
		name:        "qwen",
		backendType: "qwen-code",
		command:     "qwen",
		versionArgs: []string{"--version"},
		installCmd:  "See https://github.com/anthropics/qwen-code",
	},
	{
		name:        "opencode",
		backendType: "opencode",
		command:     "opencode",
		versionArgs: []string{"version"},
		installCmd:  "go install github.com/opencode-ai/opencode@latest",
	},
}

// checkDependencies checks required system dependencies
func checkDependencies() []Check {
	// Use default backend type for backwards compatibility
	return checkDependenciesWithBackend("claude-code")
}

// checkDependenciesWithBackend checks dependencies including backend-aware checks
func checkDependenciesWithBackend(activeBackendType string) []Check {
	checks := []Check{}

	// Check Git first (always required)
	if version := getCommandVersion("git", "--version"); version != "" {
		checks = append(checks, Check{
			Name:    "git",
			Status:  StatusOK,
			Message: version,
		})
	} else {
		checks = append(checks, Check{
			Name:    "git",
			Status:  StatusError,
			Message: "not found",
			Fix:     "brew install git",
		})
	}

	// Check gh CLI (optional, for PRs)
	if version := getCommandVersion("gh", "--version"); version != "" {
		checks = append(checks, Check{
			Name:    "gh",
			Status:  StatusOK,
			Message: version,
		})
	} else {
		checks = append(checks, Check{
			Name:    "gh",
			Status:  StatusWarning,
			Message: "not found (PR creation unavailable)",
			Fix:     "brew install gh && gh auth login",
		})
	}

	// Check all backends (active backend is required, others are optional)
	for _, backend := range backends {
		isActive := backend.backendType == activeBackendType
		version := getCommandVersion(backend.command, backend.versionArgs...)

		if version != "" {
			message := version
			if isActive {
				message = version + " [active backend]"
			}
			checks = append(checks, Check{
				Name:    backend.name,
				Status:  StatusOK,
				Message: message,
			})
		} else {
			if isActive {
				// Active backend missing is an error
				checks = append(checks, Check{
					Name:    backend.name,
					Status:  StatusError,
					Message: "not found [active backend]",
					Fix:     backend.installCmd,
				})
			} else {
				// Other backends missing is informational (skip)
				checks = append(checks, Check{
					Name:    backend.name,
					Status:  StatusDisabled,
					Message: "not installed (optional)",
				})
			}
		}
	}

	// Check Mac sleep status (macOS only)
	if runtime.GOOS == "darwin" {
		checks = append(checks, checkMacSleep())
	}

	return checks
}

// checkMacSleep checks if Mac sleep is disabled for always-on operation
func checkMacSleep() Check {
	out, err := exec.Command("pmset", "-g", "custom").Output()
	if err != nil {
		return Check{
			Name:    "sleep",
			Status:  StatusWarning,
			Message: "could not check",
		}
	}

	// Look for "sleep" setting - format is "sleep		0" or "sleep		1"
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sleep") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == "0" {
				return Check{
					Name:    "sleep",
					Status:  StatusOK,
					Message: "disabled (always-on)",
				}
			}
		}
	}

	return Check{
		Name:    "sleep",
		Status:  StatusWarning,
		Message: "enabled (Pilot may pause when idle)",
		Fix:     "pilot setup --no-sleep",
	}
}

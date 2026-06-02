package autopilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// WaitForCI polls until all required checks complete or timeout.
// Returns CISuccess if all checks pass, CIFailure if any fail,
// or error on context cancellation or timeout.
func (m *CIMonitor) WaitForCI(ctx context.Context, sha string) (CIStatus, error) {
	deadline := time.Now().Add(m.waitTimeout)
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	// Log initial status
	m.log.Info("waiting for CI", "sha", ShortSHA(sha), "timeout", m.waitTimeout, "required_checks", m.requiredChecks)

	for {
		select {
		case <-ctx.Done():
			return CIPending, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return CIPending, fmt.Errorf("CI timeout after %v", m.waitTimeout)
			}

			status, err := m.checkStatus(ctx, sha)
			if err != nil {
				m.log.Warn("CI status check failed", "error", err)
				continue
			}

			m.log.Info("CI status", "sha", ShortSHA(sha), "status", status)

			if status == CISuccess || status == CIFailure {
				return status, nil
			}
		}
	}
}

// CheckCI checks CI status once and returns immediately.
// This is the non-blocking alternative to WaitForCI.
// Returns CIPending/CIRunning if checks are still running.
func (m *CIMonitor) CheckCI(ctx context.Context, sha string) (CIStatus, error) {
	status, err := m.checkStatus(ctx, sha)
	if err != nil {
		m.log.Debug("CheckCI: status check failed",
			"sha", ShortSHA(sha),
			"error", err,
		)
		return status, err
	}

	m.log.Debug("CheckCI: status check complete",
		"sha", ShortSHA(sha),
		"status", status,
		"required_checks", m.requiredChecks,
	)
	return status, nil
}

// GetCIStatus returns the current overall CI status for a SHA.
// This is useful for point-in-time status checks without waiting.
// Deprecated: Use CheckCI instead for clarity.
func (m *CIMonitor) GetCIStatus(ctx context.Context, sha string) (CIStatus, error) {
	return m.checkStatus(ctx, sha)
}

// GetFailedChecks returns names of failed checks for a SHA.
func (m *CIMonitor) GetFailedChecks(ctx context.Context, sha string) ([]string, error) {
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return nil, err
	}

	var failed []string
	for _, run := range checkRuns.CheckRuns {
		if run.Conclusion == github.ConclusionFailure {
			failed = append(failed, run.Name)
		}
	}
	return failed, nil
}

// GetFailedCheckLogs fetches logs for all failed check runs and returns them
// as a combined string. Each check's logs are prefixed with the check name.
// Logs are truncated to maxLen total characters to keep issues readable.
// GH-1567: Include actual CI error output in fix issues.
func (m *CIMonitor) GetFailedCheckLogs(ctx context.Context, sha string, maxLen int) string {
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		m.log.Warn("failed to list check runs for log fetch", "sha", ShortSHA(sha), "error", err)
		return ""
	}

	var combined strings.Builder
	for _, run := range checkRuns.CheckRuns {
		if run.Conclusion != github.ConclusionFailure {
			continue
		}

		logs, err := m.ghClient.GetJobLogs(ctx, m.owner, m.repo, run.ID)
		if err != nil {
			m.log.Warn("failed to fetch logs for check run",
				"check", run.Name,
				"id", run.ID,
				"error", err,
			)
			continue
		}

		if combined.Len() > 0 {
			combined.WriteString("\n\n")
		}
		combined.WriteString(fmt.Sprintf("=== %s ===\n", run.Name))
		combined.WriteString(logs)

		if combined.Len() >= maxLen {
			break
		}
	}

	result := combined.String()
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

// GetCheckStatus returns the current status of a specific check by name.
func (m *CIMonitor) GetCheckStatus(ctx context.Context, sha, checkName string) (CIStatus, error) {
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return CIPending, err
	}

	for _, run := range checkRuns.CheckRuns {
		if run.Name == checkName {
			return m.mapCheckStatus(run.Status, run.Conclusion), nil
		}
	}

	return CIPending, nil
}

// GetDiscoveredChecks returns the check names discovered for a SHA.
// Returns nil if no checks have been discovered yet.
func (m *CIMonitor) GetDiscoveredChecks(sha string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.discoveredChecks[sha]
}

// SetDiscoveredChecks stores discovered check names for a SHA.
// Called during CI status checks when checks are first seen.
func (m *CIMonitor) SetDiscoveredChecks(sha string, checks []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.discoveredChecks[sha] = checks
}

// ClearDiscovery removes discovery state for a SHA.
// Should be called when a PR is removed from tracking.
func (m *CIMonitor) ClearDiscovery(sha string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.discoveredChecks, sha)
	delete(m.discoveryStart, sha)
}

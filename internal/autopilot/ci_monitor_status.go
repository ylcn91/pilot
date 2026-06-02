package autopilot

import (
	"context"
	"path"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// checkStatus gets current CI status for a SHA.
func (m *CIMonitor) checkStatus(ctx context.Context, sha string) (CIStatus, error) {
	// Get check runs (GitHub Actions)
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return CIPending, err
	}

	// Store discovered check names for later retrieval (filtered by exclusions in auto mode)
	if len(checkRuns.CheckRuns) > 0 {
		m.mu.RLock()
		_, hasDiscovered := m.discoveredChecks[sha]
		m.mu.RUnlock()

		if !hasDiscovered {
			names := make([]string, 0, len(checkRuns.CheckRuns))
			for _, run := range checkRuns.CheckRuns {
				// In auto mode, filter out excluded checks
				if m.ciChecks != nil && m.ciChecks.Mode == "auto" && m.matchesExclude(run.Name) {
					continue
				}
				names = append(names, run.Name)
			}
			if len(names) > 0 {
				m.SetDiscoveredChecks(sha, names)
				m.log.Info("discovered CI checks", "sha", ShortSHA(sha), "checks", names, "mode", m.ciChecks.Mode)
			}
		}
	}

	// Auto mode: use discovered checks with exclusions and grace period
	if m.ciChecks != nil && m.ciChecks.Mode == "auto" {
		return m.checkAutoDiscoveredRuns(ctx, sha, checkRuns)
	}

	// Manual mode: If no required checks configured, check all runs
	if len(m.requiredChecks) == 0 {
		return m.checkAllRuns(checkRuns), nil
	}

	// Track required checks
	requiredStatus := make(map[string]CIStatus)
	for _, name := range m.requiredChecks {
		requiredStatus[name] = CIPending
	}

	// Map check runs to status
	for _, run := range checkRuns.CheckRuns {
		if _, ok := requiredStatus[run.Name]; ok {
			requiredStatus[run.Name] = m.mapCheckStatus(run.Status, run.Conclusion)
		}
	}

	// Determine overall status
	return m.aggregateStatus(requiredStatus), nil
}

// checkAllRuns returns aggregate status when no required checks are configured.
func (m *CIMonitor) checkAllRuns(checkRuns *github.CheckRunsResponse) CIStatus {
	if checkRuns.TotalCount == 0 {
		return CIPending
	}

	hasFailure := false
	hasPending := false

	for _, run := range checkRuns.CheckRuns {
		status := m.mapCheckStatus(run.Status, run.Conclusion)
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	// B4 (TASK-345): only declare failure once no check is still pending — a
	// fail-fast matrix leg or flaky check can report failure before siblings
	// finish; wait (CIPending) so the suite completes / flaky checks auto-rerun.
	if hasFailure && !hasPending {
		return CIFailure
	}
	if hasPending {
		return CIPending
	}
	return CISuccess
}

// checkAutoDiscoveredRuns checks CI status in auto mode with exclusion filtering.
// It waits during the grace period if no checks are found yet, then falls back
// to the commit-status API before treating a SHA as having no CI configured.
func (m *CIMonitor) checkAutoDiscoveredRuns(ctx context.Context, sha string, checkRuns *github.CheckRunsResponse) (CIStatus, error) {
	// Filter checks by exclusion patterns
	var filteredRuns []github.CheckRun
	for _, run := range checkRuns.CheckRuns {
		if !m.matchesExclude(run.Name) {
			filteredRuns = append(filteredRuns, run)
		}
	}

	// Handle grace period for check discovery
	if len(filteredRuns) == 0 {
		m.mu.Lock()
		startTime, exists := m.discoveryStart[sha]
		if !exists {
			// First check: start the grace period
			m.discoveryStart[sha] = time.Now()
			m.mu.Unlock()
			m.log.Debug("no CI checks found, starting grace period",
				"sha", ShortSHA(sha),
				"grace_period", m.ciChecks.DiscoveryGracePeriod,
			)
			return CIPending, nil
		}
		m.mu.Unlock()

		// Check if grace period has expired
		elapsed := time.Since(startTime)
		if elapsed < m.ciChecks.DiscoveryGracePeriod {
			m.log.Debug("waiting for CI checks during grace period",
				"sha", ShortSHA(sha),
				"elapsed", elapsed,
				"remaining", m.ciChecks.DiscoveryGracePeriod-elapsed,
			)
			return CIPending, nil
		}

		// Grace period expired with no check runs — query commit-status API before
		// concluding that no CI is configured. Providers like CircleCI, Jenkins,
		// Travis, and Buildkite report exclusively via the statuses API.
		combined, err := m.ghClient.GetCombinedStatus(ctx, m.owner, m.repo, sha)
		if err != nil {
			m.log.Warn("grace period expired; combined-status lookup failed, treating as no CI",
				"sha", ShortSHA(sha),
				"error", err,
			)
			return CISuccess, nil
		}
		status := m.mapCombinedStatus(combined)
		m.log.Info("grace period expired with no check runs; using commit-status API",
			"sha", ShortSHA(sha),
			"combined_state", combined.State,
			"total_count", combined.TotalCount,
			"status", status,
		)
		return status, nil
	}

	// Clear discovery start since we found checks
	m.mu.Lock()
	delete(m.discoveryStart, sha)
	m.mu.Unlock()

	// Aggregate status from filtered runs
	hasFailure := false
	hasPending := false

	for _, run := range filteredRuns {
		status := m.mapCheckStatus(run.Status, run.Conclusion)
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	// B4 (TASK-345): only declare failure once no check is still pending — a
	// fail-fast matrix leg or flaky check can report failure before siblings
	// finish; wait (CIPending) so the suite completes / flaky checks auto-rerun.
	if hasFailure && !hasPending {
		return CIFailure, nil
	}
	if hasPending {
		return CIPending, nil
	}
	return CISuccess, nil
}

// mapCombinedStatus converts a GitHub combined commit-status response into CIStatus.
// TotalCount==0 means no status contexts exist → genuine no-CI repo → CISuccess.
func (m *CIMonitor) mapCombinedStatus(combined *github.CombinedStatus) CIStatus {
	if combined.TotalCount == 0 {
		return CISuccess
	}
	switch combined.State {
	case github.StatusFailure, github.StatusError:
		return CIFailure
	case github.StatusPending:
		return CIPending
	default:
		return CISuccess
	}
}

// matchesExclude checks if a check name matches any exclusion pattern.
// Supports glob patterns using path.Match (e.g., "codecov/*", "*.optional").
func (m *CIMonitor) matchesExclude(name string) bool {
	if m.ciChecks == nil || len(m.ciChecks.Exclude) == 0 {
		return false
	}

	for _, pattern := range m.ciChecks.Exclude {
		// Try exact match first
		if pattern == name {
			return true
		}
		// Try glob match
		if matched, err := path.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}

// aggregateStatus determines overall status from individual check statuses.
func (m *CIMonitor) aggregateStatus(statuses map[string]CIStatus) CIStatus {
	hasFailure := false
	hasPending := false

	for _, status := range statuses {
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	// B4 (TASK-345): only declare failure once no check is still pending — a
	// fail-fast matrix leg or flaky check can report failure before siblings
	// finish; wait (CIPending) so the suite completes / flaky checks auto-rerun.
	if hasFailure && !hasPending {
		return CIFailure
	}
	if hasPending {
		return CIPending
	}
	return CISuccess
}

// mapCheckStatus maps GitHub check status to CIStatus.
func (m *CIMonitor) mapCheckStatus(status, conclusion string) CIStatus {
	switch status {
	case github.CheckRunQueued, github.CheckRunInProgress:
		return CIRunning
	case github.CheckRunCompleted:
		switch conclusion {
		case github.ConclusionSuccess:
			return CISuccess
		case github.ConclusionFailure, github.ConclusionCancelled, github.ConclusionTimedOut:
			return CIFailure
		case github.ConclusionSkipped, github.ConclusionNeutral:
			// Skipped/neutral checks don't block
			return CISuccess
		default:
			return CIPending
		}
	default:
		return CIPending
	}
}

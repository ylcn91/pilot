package autopilot

import (
	"fmt"
)

// persistPRState saves a PR state to the store if available.
//
// TASK-324 concurrency contract: this method is LOCK-FREE with respect to the
// per-PR mutex. The CALLER MUST hold prState.mu (so the fields read by
// stateStore.SavePRState are stable) — OR the prState must not yet be published in
// c.activePRs (e.g. freshly constructed). It must NOT take prState.mu itself: every
// caller that holds the live pointer already owns prState.mu, and re-locking would
// deadlock (Go's sync.Mutex is non-reentrant).
func (c *Controller) persistPRState(prState *PRState) {
	if c.stateStore == nil {
		return
	}
	if err := c.stateStore.SavePRState(prState); err != nil {
		c.log.Warn("failed to persist PR state", "pr", prState.PRNumber, "error", err)
	}
}

// persistRemovePR removes a PR state from the store if available.
func (c *Controller) persistRemovePR(prNumber int) {
	if c.stateStore == nil {
		return
	}
	if err := c.stateStore.RemovePRState(prNumber); err != nil {
		c.log.Warn("failed to remove persisted PR state", "pr", prNumber, "error", err)
	}
}

// persistPRFailures saves per-PR failure state to the store if available.
func (c *Controller) persistPRFailures(prNumber int, state *prFailureState) {
	if c.stateStore == nil {
		return
	}
	if err := c.stateStore.SavePRFailures(prNumber, state.FailureCount, state.LastFailureTime); err != nil {
		c.log.Warn("failed to persist PR failure state", "pr", prNumber, "error", err)
	}
}

// removePRFailures removes per-PR failure state from the store if available.
func (c *Controller) removePRFailures(prNumber int) {
	if c.stateStore == nil {
		return
	}
	if err := c.stateStore.RemovePRFailures(prNumber); err != nil {
		c.log.Warn("failed to remove PR failure state", "pr", prNumber, "error", err)
	}
}

// RestoreState loads PR states and per-PR failures from the persistent store.
// If state is found in the store, ScanExistingPRs is unnecessary.
// Returns the number of restored PRs.
func (c *Controller) RestoreState() (int, error) {
	if c.stateStore == nil {
		return 0, nil
	}

	// Restore PR states
	states, err := c.stateStore.LoadAllPRStates()
	if err != nil {
		return 0, fmt.Errorf("failed to load PR states: %w", err)
	}

	c.mu.Lock()
	for _, pr := range states {
		// Skip terminal states — they shouldn't be active
		if pr.Stage == StageFailed {
			continue
		}
		c.activePRs[pr.PRNumber] = pr
	}
	c.mu.Unlock()

	// Restore per-PR failures
	prFailures, err := c.stateStore.LoadAllPRFailures()
	if err != nil {
		c.log.Warn("failed to load per-PR failure states", "error", err)
	} else {
		c.mu.Lock()
		for prNum, state := range prFailures {
			c.prFailures[prNum] = state
		}
		c.mu.Unlock()
	}

	restored := len(states)
	if restored > 0 {
		c.log.Info("restored autopilot state from SQLite",
			"pr_states", restored,
			"pr_failures", len(prFailures),
		)
	}

	return restored, nil
}

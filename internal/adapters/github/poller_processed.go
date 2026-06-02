package github

import (
	"log/slog"
	"strconv"
	"time"
)

// markProcessed marks an issue as processed with the current timestamp
func (p *Poller) markProcessed(number int) {
	p.mu.Lock()
	p.processed[number] = time.Now()
	p.mu.Unlock()

	// Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("github", p.repoKey(), strconv.Itoa(number)); err != nil {
			p.logger.Warn("Failed to persist processed issue", slog.Int("issue", number), slog.Any("error", err))
		}
	}
}

// unmarkProcessed removes an issue from the processed set, allowing retry.
// GH-2176: Used when execution fails without creating a PR.
func (p *Poller) unmarkProcessed(number int) {
	p.mu.Lock()
	delete(p.processed, number)
	p.mu.Unlock()

	if p.processedStore != nil {
		if err := p.processedStore.Unmark("github", p.repoKey(), strconv.Itoa(number)); err != nil {
			p.logger.Warn("Failed to unmark processed issue", slog.Int("issue", number), slog.Any("error", err))
		}
	}
}

// Drain stops accepting new issues and waits for active executions to finish.
// Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new issues will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// Used in tests to synchronize after checkForNewIssues.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

// IsProcessed checks if an issue has been processed
func (p *Poller) IsProcessed(number int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.processed[number]
	return ok
}

// ProcessedCount returns the number of processed issues
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed issues map
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[int]time.Time)
	p.mu.Unlock()
}

// MarkProcessed marks an issue as processed so findOldestUnprocessedIssue skips it.
// Called by the executor after epic sub-issues are created to prevent re-dispatch (GH-3240).
func (p *Poller) MarkProcessed(number int) {
	p.markProcessed(number)
}

// ClearProcessed removes a single issue from the processed map.
// Used by the stale label cleaner when removing pilot-failed labels
// to allow the issue to be retried without restarting Pilot.
func (p *Poller) ClearProcessed(number int) {
	p.mu.Lock()
	delete(p.processed, number)
	p.mu.Unlock()

	// Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("github", p.repoKey(), strconv.Itoa(number)); err != nil {
			p.logger.Warn("Failed to unmark issue in store",
				slog.Int("number", number),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared issue from processed map",
		slog.Int("number", number))
}

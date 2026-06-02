package jira

import (
	"log/slog"
)

func (p *Poller) markProcessed(key string) {
	p.mu.Lock()
	p.processed[key] = true
	p.mu.Unlock()

	// GH-1357: Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("jira", "", key); err != nil {
			p.logger.Warn("Failed to persist processed issue", slog.String("issue", key), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if an issue has been processed
func (p *Poller) IsProcessed(key string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[key]
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
	p.processed = make(map[string]bool)
	p.mu.Unlock()
}

// ClearProcessed removes a specific issue from the processed map (for retry)
func (p *Poller) ClearProcessed(key string) {
	p.mu.Lock()
	delete(p.processed, key)
	p.mu.Unlock()

	// GH-1357: Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("jira", "", key); err != nil {
			p.logger.Warn("Failed to unmark issue in store",
				slog.String("key", key),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared issue from processed map",
		slog.String("key", key))
}

// Drain stops accepting new issues and waits for active executions to finish.
// GH-1357: Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new issues will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// GH-1357: Used in tests to synchronize after checkForNewIssues.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

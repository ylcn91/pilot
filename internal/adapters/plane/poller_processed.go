package plane

import (
	"log/slog"
)

func (p *Poller) markProcessed(id string) {
	p.mu.Lock()
	p.processed[id] = true
	p.mu.Unlock()

	// GH-1830: Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("plane", "", id); err != nil {
			p.logger.Warn("Failed to persist processed issue", slog.String("id", id), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if a work item has been processed.
func (p *Poller) IsProcessed(id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[id]
}

// ProcessedCount returns the number of processed work items.
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed work items map.
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[string]bool)
	p.mu.Unlock()
}

// ClearProcessed removes a specific work item from the processed map (for retry).
// GH-1830: Used when pilot-failed label is removed to allow retry.
func (p *Poller) ClearProcessed(id string) {
	p.mu.Lock()
	delete(p.processed, id)
	p.mu.Unlock()

	// Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("plane", "", id); err != nil {
			p.logger.Warn("Failed to unmark issue in store",
				slog.String("id", id),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared issue from processed map",
		slog.String("id", id))
}

// Drain stops accepting new work items and waits for active executions to finish.
// GH-1830: Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new work items will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// GH-1830: Used in tests to synchronize after checkForNewIssues.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

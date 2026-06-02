package asana

import (
	"log/slog"
)

func (p *Poller) markProcessed(gid string) {
	p.mu.Lock()
	p.processed[gid] = true
	p.mu.Unlock()

	// GH-1359: Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("asana", "", gid); err != nil {
			p.logger.Warn("Failed to persist processed task", slog.String("gid", gid), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if a task has been processed
func (p *Poller) IsProcessed(gid string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[gid]
}

// ProcessedCount returns the number of processed tasks
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed tasks map
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[string]bool)
	p.mu.Unlock()
}

// ClearProcessed removes a single task from the processed map.
// GH-1359: Used when pilot-failed tag is removed to allow the task to be retried.
func (p *Poller) ClearProcessed(gid string) {
	p.mu.Lock()
	delete(p.processed, gid)
	p.mu.Unlock()

	// Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("asana", "", gid); err != nil {
			p.logger.Warn("Failed to unmark task in store",
				slog.String("gid", gid),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared task from processed map",
		slog.String("gid", gid))
}

// Drain stops accepting new tasks and waits for active executions to finish.
// GH-1359: Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new tasks will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// GH-1359: Used in tests to synchronize after checkForNewTasks.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

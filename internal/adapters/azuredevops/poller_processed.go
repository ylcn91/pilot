package azuredevops

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
)

// markProcessed marks a work item as processed
func (p *Poller) markProcessed(id int) {
	p.mu.Lock()
	p.processed[id] = true
	p.mu.Unlock()

	// GH-1358: Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("azuredevops", p.repoKey, strconv.Itoa(id)); err != nil {
			p.logger.Warn("Failed to persist processed work item", slog.Int("id", id), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if a work item has been processed
func (p *Poller) IsProcessed(id int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[id]
}

// ProcessedCount returns the number of processed work items
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed work items map
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[int]bool)
	p.mu.Unlock()
}

// ClearProcessed removes a single work item from the processed map.
// GH-1358: Used when pilot-failed tag is removed to allow the work item to be retried.
func (p *Poller) ClearProcessed(id int) {
	p.mu.Lock()
	delete(p.processed, id)
	p.mu.Unlock()

	// Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("azuredevops", p.repoKey, strconv.Itoa(id)); err != nil {
			p.logger.Warn("Failed to unmark work item in store",
				slog.Int("id", id),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared work item from processed map",
		slog.Int("id", id))
}

// Drain stops accepting new work items and waits for active executions to finish.
// GH-1358: Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new work items will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// GH-1358: Used in tests to synchronize after checkForNewWorkItems.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

// ExtractPRNumber extracts PR ID from an Azure DevOps PR URL
// e.g., "https://dev.azure.com/org/project/_git/repo/pullrequest/123" -> 123
func ExtractPRNumber(prURL string) (int, error) {
	if prURL == "" {
		return 0, fmt.Errorf("empty PR URL")
	}

	// Match pattern: /pullrequest/123
	re := regexp.MustCompile(`/pullrequest/(\d+)`)
	matches := re.FindStringSubmatch(prURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not extract PR number from URL: %s", prURL)
	}

	var num int
	if _, err := fmt.Sscanf(matches[1], "%d", &num); err != nil {
		return 0, fmt.Errorf("invalid PR number in URL: %s", prURL)
	}

	return num, nil
}

package bitbucket

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
)

// markProcessed marks an issue as processed
func (p *Poller) markProcessed(id int) {
	p.mu.Lock()
	p.processed[id] = true
	p.mu.Unlock()

	if p.processedStore != nil {
		if err := p.processedStore.Mark("bitbucket", p.repoKey, strconv.Itoa(id)); err != nil {
			p.logger.Warn("Failed to persist processed issue", slog.Int("id", id), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if an issue has been processed
func (p *Poller) IsProcessed(id int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[id]
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
	p.processed = make(map[int]bool)
	p.mu.Unlock()
}

// ClearProcessed removes a single issue from the processed map.
func (p *Poller) ClearProcessed(id int) {
	p.mu.Lock()
	delete(p.processed, id)
	p.mu.Unlock()

	if p.processedStore != nil {
		if err := p.processedStore.Unmark("bitbucket", p.repoKey, strconv.Itoa(id)); err != nil {
			p.logger.Warn("Failed to unmark issue in store",
				slog.Int("id", id),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared issue from processed map", slog.Int("id", id))
}

// Drain stops accepting new issues and waits for active executions to finish.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new issues will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

// ExtractPRNumber extracts the PR ID from a Bitbucket Cloud PR URL.
// e.g. "https://bitbucket.org/workspace/repo/pull-requests/123" -> 123
func ExtractPRNumber(prURL string) (int, error) {
	if prURL == "" {
		return 0, fmt.Errorf("empty PR URL")
	}

	// Bitbucket Cloud uses the "/pull-requests/{id}" path segment.
	re := regexp.MustCompile(`/pull-requests/(\d+)`)
	matches := re.FindStringSubmatch(prURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not extract PR number from URL: %s", prURL)
	}

	num, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid PR number in URL: %s", prURL)
	}

	return num, nil
}

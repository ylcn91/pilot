package gitlab

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
)

// markProcessed marks an issue as processed
func (p *Poller) markProcessed(iid int) {
	p.mu.Lock()
	p.processed[iid] = true
	p.mu.Unlock()

	// GH-1358: Persist to store if available
	if p.processedStore != nil {
		if err := p.processedStore.Mark("gitlab", p.repoKey, strconv.Itoa(iid)); err != nil {
			p.logger.Warn("Failed to persist processed issue", slog.Int("iid", iid), slog.Any("error", err))
		}
	}
}

// IsProcessed checks if an issue has been processed
func (p *Poller) IsProcessed(iid int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[iid]
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
// GH-1358: Used when pilot-failed label is removed to allow the issue to be retried.
func (p *Poller) ClearProcessed(iid int) {
	p.mu.Lock()
	delete(p.processed, iid)
	p.mu.Unlock()

	// Also clear from persistent store
	if p.processedStore != nil {
		if err := p.processedStore.Unmark("gitlab", p.repoKey, strconv.Itoa(iid)); err != nil {
			p.logger.Warn("Failed to unmark issue in store",
				slog.Int("iid", iid),
				slog.Any("error", err))
		}
	}

	p.logger.Debug("Cleared issue from processed map",
		slog.Int("iid", iid))
}

// Drain stops accepting new issues and waits for active executions to finish.
// GH-1358: Used during hot upgrade to let in-flight work complete before process restart.
func (p *Poller) Drain() {
	p.logger.Info("Draining poller — no new issues will be accepted")
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
	p.logger.Info("Poller drained — all active tasks completed")
}

// WaitForActive waits for all active parallel goroutines to finish.
// GH-1358: Used in tests to synchronize after checkForNewIssues.
func (p *Poller) WaitForActive() {
	p.wgMu.Lock()
	p.stopping.Store(true)
	p.wgMu.Unlock()
	p.activeWg.Wait()
}

// ExtractMRNumber extracts MR IID from a GitLab MR URL
// e.g., "https://gitlab.com/namespace/project/-/merge_requests/123" -> 123
func ExtractMRNumber(mrURL string) (int, error) {
	if mrURL == "" {
		return 0, fmt.Errorf("empty MR URL")
	}

	// Match pattern: /-/merge_requests/123 or /merge_requests/123
	re := regexp.MustCompile(`/(?:-/)?merge_requests/(\d+)`)
	matches := re.FindStringSubmatch(mrURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not extract MR number from URL: %s", mrURL)
	}

	var num int
	if _, err := fmt.Sscanf(matches[1], "%d", &num); err != nil {
		return 0, fmt.Errorf("invalid MR number in URL: %s", mrURL)
	}

	return num, nil
}

package github

import (
	"context"
	"log/slog"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/text"
)

// groupByOverlappingScope partitions issues into groups where members reference
// at least one common directory (transitive closure). Within each group only the
// oldest issue should be dispatched to avoid merge conflicts.
func groupByOverlappingScope(candidates []*Issue) [][]*Issue {
	n := len(candidates)
	if n == 0 {
		return nil
	}

	// Union-Find
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Pre-extract directories once per candidate, then pairwise set intersection.
	// Comment bodies are attacker-controllable text; sanitize before parsing so
	// smuggled paths cannot influence grouping decisions.
	dirs := make([]map[string]bool, n)
	for i, c := range candidates {
		dirs[i] = executor.ExtractDirectoriesFromText(text.SanitizeUntrustedString(c.Body))
	}
	for i := 0; i < n; i++ {
		if len(dirs[i]) == 0 {
			continue
		}
		for j := i + 1; j < n; j++ {
			if len(dirs[j]) == 0 {
				continue
			}
			for d := range dirs[i] {
				if dirs[j][d] {
					union(i, j)
					break
				}
			}
		}
	}

	// Collect groups
	groups := make(map[int][]*Issue)
	for i, c := range candidates {
		root := find(i)
		groups[root] = append(groups[root], c)
	}

	result := make([][]*Issue, 0, len(groups))
	for _, g := range groups {
		result = append(result, g)
	}
	return result
}

// repoKey returns "owner/repo" for use as the Prometheus `repo` label.
func (p *Poller) repoKey() string { return p.owner + "/" + p.repo }

// recordSkip increments the skip counter when pollerMetrics is configured.
func (p *Poller) recordSkip(reason string) {
	if p.pollerMetrics != nil {
		p.pollerMetrics.RecordPollerSkipped(p.repoKey(), reason)
	}
}

// recordDispatched increments the dispatch counter when pollerMetrics is configured.
func (p *Poller) recordDispatched() {
	if p.pollerMetrics != nil {
		p.pollerMetrics.RecordPollerDispatched(p.repoKey())
	}
}

// recordDeferredScopeOverlap increments the scope-overlap deferral counter when pollerMetrics is configured.
func (p *Poller) recordDeferredScopeOverlap() {
	if p.pollerMetrics != nil {
		p.pollerMetrics.RecordPollerDeferredScopeOverlap(p.repoKey())
	}
}

// syncBoardStatusInProgress moves the issue card to the configured in-progress status
// on the Projects V2 board. Called once after confirmed dispatch, before execution starts.
// Logs errors but does not fail the dispatch — board sync is best-effort.
// No-op when boardSync is nil or inProgressStatus is empty.
func (p *Poller) syncBoardStatusInProgress(ctx context.Context, issue *Issue) {
	if p.board.boardSync == nil || p.board.inProgressStatus == "" {
		return
	}

	nodeID := issue.NodeID
	if nodeID == "" {
		var err error
		nodeID, err = p.client.GetIssueNodeID(ctx, p.owner, p.repo, issue.Number)
		if err != nil {
			p.logger.Warn("board sync: failed to resolve issue node ID",
				slog.Int("issue", issue.Number),
				slog.Any("error", err))
			return
		}
	}

	if err := p.board.boardSync.UpdateProjectItemStatus(ctx, nodeID, p.board.inProgressStatus); err != nil {
		p.logger.Warn("board sync: failed to update project item status",
			slog.Int("issue", issue.Number),
			slog.String("status", p.board.inProgressStatus),
			slog.Any("error", err))
	}
}

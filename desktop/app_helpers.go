package main

import (
	"fmt"
	"strings"
)

// issueIDFromTaskID extracts the short issue ID from a task ID string.
// Task IDs typically look like "GH-123" or "LINEAR-456".
func issueIDFromTaskID(taskID string) string {
	parts := strings.SplitN(taskID, "/", 2)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return taskID
}

// defaultIssueRepo is the "owner/repo" used to build GitHub issue URLs when no
// repo is configured. Matches the gateway default fallback in startup().
const defaultIssueRepo = "ylcn91/pilot"

// issueURL constructs the GitHub issue URL from a task ID for the given
// "owner/repo". An empty repo falls back to defaultIssueRepo so behavior is
// unchanged when config provides no GitHub repo.
func issueURL(taskID, repo string) string {
	if repo == "" {
		repo = defaultIssueRepo
	}
	id := issueIDFromTaskID(taskID)
	if strings.HasPrefix(id, "GH-") {
		num := strings.TrimPrefix(id, "GH-")
		return fmt.Sprintf("https://github.com/%s/issues/%s", repo, num)
	}
	return ""
}

// queueStatusPriority returns a numeric priority for queue task statuses.
// Higher value = better status to keep when deduplicating.
func queueStatusPriority(status string) int {
	switch status {
	case "running":
		return 3
	case "done":
		return 2
	case "queued", "pending":
		return 1
	default: // failed
		return 0
	}
}

// queueTaskBetter returns true if candidate should replace existing in dedup.
func queueTaskBetter(candidate, existing QueueTask) bool {
	cp, ep := queueStatusPriority(candidate.Status), queueStatusPriority(existing.Status)
	if cp != ep {
		return cp > ep
	}
	return candidate.CreatedAt.After(existing.CreatedAt)
}

// historyEntryBetter returns true if candidate should replace existing in dedup.
func historyEntryBetter(candidate, existing HistoryEntry) bool {
	// completed beats failed
	if candidate.Status != existing.Status {
		if candidate.Status == "completed" {
			return true
		}
		if existing.Status == "completed" {
			return false
		}
	}
	// prefer entry with PR URL
	if candidate.PRURL != "" && existing.PRURL == "" {
		return true
	}
	if candidate.PRURL == "" && existing.PRURL != "" {
		return false
	}
	// most recent wins
	return candidate.CompletedAt.After(existing.CompletedAt)
}

// normalizeStatus maps internal execution statuses to frontend-friendly names.
func normalizeStatus(status string) string {
	switch status {
	case "completed":
		return "done"
	case "running":
		return "running"
	case "queued":
		return "queued"
	case "pending":
		return "pending"
	case "failed":
		return "failed"
	default:
		return status
	}
}

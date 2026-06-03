package bitbucket

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/text"
)

// TaskInfo contains the extracted task information from a Bitbucket issue
type TaskInfo struct {
	ID          string
	Title       string
	Description string
	Priority    Priority
	Labels      []string
	Workspace   string
	Repo        string
	IssueID     int
	IssueURL    string
	CloneURL    string
}

// synthesizeLabels builds a label-like slice from a Bitbucket issue's kind and
// priority fields, since the Cloud issue tracker has no free-form label list.
func synthesizeLabels(issue *Issue) []string {
	var labels []string
	if issue.Kind != "" {
		labels = append(labels, issue.Kind)
	}
	if issue.Priority != "" {
		labels = append(labels, "priority::"+issue.Priority)
	}
	return labels
}

// issueBody returns the raw content of an issue, tolerating a nil Content.
func issueBody(issue *Issue) string {
	if issue.Content == nil {
		return ""
	}
	return issue.Content.Raw
}

// issueURL returns the html link of an issue, tolerating missing links.
func issueURL(issue *Issue) string {
	if issue.Links != nil && issue.Links.HTML != nil {
		return issue.Links.HTML.Href
	}
	return ""
}

// ConvertIssueToTask converts a Bitbucket issue to a TaskInfo.
//
// All untrusted fields (Title, Description) are run through
// text.SanitizeUntrusted to strip invisible Unicode format characters
// used for ASCII-smuggling / prompt-injection attacks.
func ConvertIssueToTask(issue *Issue, repo *Repository) *TaskInfo {
	var cloneURL, fullName string
	if repo != nil {
		fullName = repo.FullName
		if repo.Links != nil && repo.Links.HTML != nil {
			cloneURL = repo.Links.HTML.Href + ".git"
		}
	}

	workspace, repoSlug := splitFullName(fullName)

	title, titleStripped := text.SanitizeUntrusted(issue.Title)
	description, bodyStripped := text.SanitizeUntrusted(extractDescription(issueBody(issue)))

	if titleStripped+bodyStripped > 0 {
		logging.WithComponent("bitbucket").Warn(
			"invisible_unicode_stripped",
			slog.String("source", "bitbucket"),
			slog.Int("issue", issue.ID),
			slog.Int("title_stripped", titleStripped),
			slog.Int("body_stripped", bodyStripped),
		)
	}

	labels := issue.Labels
	if labels == nil {
		labels = synthesizeLabels(issue)
	}

	task := &TaskInfo{
		ID:          fmt.Sprintf("BB-%d", issue.ID),
		Title:       title,
		Description: description,
		Priority:    extractPriority(labels),
		Labels:      extractLabelNames(labels),
		Workspace:   workspace,
		Repo:        repoSlug,
		IssueID:     issue.ID,
		IssueURL:    issueURL(issue),
		CloneURL:    cloneURL,
	}

	return task
}

// splitFullName splits a "workspace/repo" full name into its parts.
func splitFullName(fullName string) (workspace, repo string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return fullName, ""
}

// extractDescription extracts and cleans the task description
func extractDescription(body string) string {
	if body == "" {
		return ""
	}

	lines := strings.Split(body, "\n")
	var filtered []string
	skipSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip common issue template sections that aren't useful for tasks
		if strings.HasPrefix(trimmed, "### Checklist") ||
			strings.HasPrefix(trimmed, "### Environment") ||
			strings.HasPrefix(trimmed, "### Bug Report") {
			skipSection = true
			continue
		}

		// Resume at next heading
		if skipSection && strings.HasPrefix(trimmed, "### ") {
			skipSection = false
		}

		if !skipSection {
			filtered = append(filtered, line)
		}
	}

	return text.SanitizeUntrustedString(strings.TrimSpace(strings.Join(filtered, "\n")))
}

// extractPriority determines priority from labels
func extractPriority(labels []string) Priority {
	for _, label := range labels {
		name := strings.ToLower(label)

		// Common priority label patterns (Bitbucket kinds/priorities are mapped
		// into label-like strings by synthesizeLabels, e.g. priority::critical).
		if strings.Contains(name, "urgent") || strings.Contains(name, "critical") ||
			strings.Contains(name, "blocker") || name == "p0" || name == "priority::urgent" {
			return PriorityUrgent
		}
		if strings.Contains(name, "high") || strings.Contains(name, "major") ||
			name == "p1" || name == "priority::high" {
			return PriorityHigh
		}
		if strings.Contains(name, "medium") || name == "p2" || name == "priority::medium" {
			return PriorityMedium
		}
		if strings.Contains(name, "low") || strings.Contains(name, "minor") ||
			strings.Contains(name, "trivial") || name == "p3" || name == "priority::low" {
			return PriorityLow
		}
	}

	return PriorityNone
}

// extractLabelNames returns a list of label names excluding pilot/priority labels
func extractLabelNames(labels []string) []string {
	var names []string
	for _, label := range labels {
		name := strings.ToLower(label)
		// Skip pilot and priority labels
		if strings.HasPrefix(name, "pilot") ||
			strings.HasPrefix(name, "priority") ||
			strings.HasPrefix(name, "p0") || strings.HasPrefix(name, "p1") ||
			strings.HasPrefix(name, "p2") || strings.HasPrefix(name, "p3") {
			continue
		}
		names = append(names, label)
	}
	return names
}

// ExtractAcceptanceCriteria extracts acceptance criteria from issue body
func ExtractAcceptanceCriteria(body string) []string {
	var criteria []string

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)### acceptance criteria\s*\n([\s\S]*?)(?:\n###|\z)`),
		regexp.MustCompile(`(?i)### criteria\s*\n([\s\S]*?)(?:\n###|\z)`),
		regexp.MustCompile(`(?i)## acceptance criteria\s*\n([\s\S]*?)(?:\n##|\z)`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(body)
		if len(matches) > 1 {
			checkboxPattern := regexp.MustCompile(`- \[[ x]\] (.+)`)
			items := checkboxPattern.FindAllStringSubmatch(matches[1], -1)
			for _, item := range items {
				if len(item) > 1 {
					criteria = append(criteria, strings.TrimSpace(item[1]))
				}
			}
			if len(criteria) == 0 {
				listPattern := regexp.MustCompile(`- (.+)`)
				items = listPattern.FindAllStringSubmatch(matches[1], -1)
				for _, item := range items {
					if len(item) > 1 {
						criteria = append(criteria, strings.TrimSpace(item[1]))
					}
				}
			}
			break
		}
	}

	return criteria
}

// BuildTaskPrompt creates a prompt for Claude Code from the task info
func BuildTaskPrompt(task *TaskInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("**Issue**: %s\n", task.IssueURL))
	sb.WriteString(fmt.Sprintf("**Priority**: %s\n\n", PriorityName(task.Priority)))

	if task.Description != "" {
		sb.WriteString("## Description\n\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	criteria := ExtractAcceptanceCriteria(task.Description)
	if len(criteria) > 0 {
		sb.WriteString("## Acceptance Criteria\n\n")
		for _, c := range criteria {
			sb.WriteString(fmt.Sprintf("- [ ] %s\n", c))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Requirements\n\n")
	sb.WriteString("1. Implement the changes described above\n")
	sb.WriteString("2. Write tests for new functionality\n")
	sb.WriteString("3. Ensure all existing tests pass\n")
	sb.WriteString("4. Follow the project's code style and conventions\n")

	return sb.String()
}

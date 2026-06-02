package executor

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// projectContextBudget caps the whole project-context block injected into the
// executor prompt. Sized to keep priming useful without crowding out the task.
const projectContextBudget = 6000

// loadProjectContext assembles the project-context block injected into Navigator
// prompts. It prefers a curated .agent/system/PRIMING.md (used verbatim); absent
// that, it scrapes key sections from DEVELOPMENT-README.md and appends
// system/ARCHITECTURE.md when present. The result is capped at one overall
// budget, truncated on a heading/line boundary. Signature is stable: callers in
// epic.go depend on loadProjectContext(agentDir) string.
func loadProjectContext(agentDir string) string {
	// (a) Curated priming wins — load it verbatim, no heading slicing.
	if priming, err := os.ReadFile(filepath.Join(agentDir, "system", "PRIMING.md")); err == nil {
		return capProjectContext(strings.TrimSpace(string(priming)))
	}

	// (b) Fall back to scraping DEVELOPMENT-README.md.
	readmePath := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}

	text := string(content)
	var sb strings.Builder

	// Extract Key Components table (~500 tokens)
	if components := extractSection(text, "### Key Components", "### "); components != "" {
		sb.WriteString("### Key Components\n\n")
		sb.WriteString(components)
		sb.WriteString("\n\n")
	}

	// Extract Key Files section (~800 tokens)
	if files := extractSection(text, "## Key Files", "## "); files != "" {
		sb.WriteString("## Key Files\n\n")
		sb.WriteString(files)
		sb.WriteString("\n\n")
	}

	// Extract Project Structure (~300 tokens)
	if structure := extractSection(text, "## Project Structure", "## "); structure != "" {
		sb.WriteString("## Project Structure\n\n")
		sb.WriteString(structure)
		sb.WriteString("\n\n")
	}

	// Extract Current Version (~200 tokens) - just the line
	if versionStart := strings.Index(text, "**Current Version:"); versionStart != -1 {
		versionLine := text[versionStart:]
		if newlineIdx := strings.Index(versionLine, "\n"); newlineIdx != -1 {
			versionLine = versionLine[:newlineIdx]
		}
		sb.WriteString(strings.TrimSpace(versionLine))
		sb.WriteString("\n\n")
	}

	// (c) Append ARCHITECTURE.md when present for system-level grounding.
	if arch, err := os.ReadFile(filepath.Join(agentDir, "system", "ARCHITECTURE.md")); err == nil {
		sb.WriteString("## Architecture\n\n")
		sb.WriteString(strings.TrimSpace(string(arch)))
		sb.WriteString("\n\n")
	}

	return capProjectContext(strings.TrimSpace(sb.String()))
}

// capProjectContext enforces projectContextBudget, truncating on a heading/line
// boundary and warning when the block overflows so over-large context is visible.
func capProjectContext(block string) string {
	if len(block) <= projectContextBudget {
		return block
	}

	cut := block[:projectContextBudget]
	// Prefer the last Markdown heading so we don't sever a section mid-body.
	bound := strings.LastIndex(cut, "\n#")
	if bound <= 0 {
		bound = strings.LastIndexByte(cut, '\n')
	}
	if bound > 0 {
		cut = cut[:bound]
	}

	slog.Warn("project_context_truncated",
		slog.String("component", "executor"),
		slog.Int("original_chars", len(block)),
		slog.Int("budget", projectContextBudget),
	)

	return strings.TrimRight(cut, " \n\t") + "\n\n… (project context truncated)"
}

// extractSection extracts content between a start marker and the next occurrence of end marker
func extractSection(text, startMarker, endMarker string) string {
	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return ""
	}

	// Find content after the start marker
	contentStart := startIdx + len(startMarker)
	remaining := text[contentStart:]

	// Find the end boundary - look for next section with same level
	// Use newline + endMarker to ensure we match section headers at line start,
	// not substrings within headers (e.g., "## " within "### ")
	endIdx := len(remaining)
	if endMarker != "" {
		lineMarker := "\n" + endMarker
		if nextIdx := strings.Index(remaining, lineMarker); nextIdx != -1 {
			endIdx = nextIdx
		}
	}

	result := strings.TrimSpace(remaining[:endIdx])

	// Limit to reasonable size to prevent prompt bloat
	if len(result) > 2000 {
		result = result[:2000] + "..."
	}

	return result
}

// findRelevantSOPs scans .agent/sops/ for files matching task keywords
// Returns up to 3 relevant SOP file paths.
func findRelevantSOPs(agentDir string, taskDescription string) []string {
	sopsDir := filepath.Join(agentDir, "sops")
	if _, err := os.Stat(sopsDir); err != nil {
		return nil
	}

	// Extract keywords from task description (simple approach)
	keywords := extractTaskKeywords(taskDescription)
	if len(keywords) == 0 {
		return nil
	}

	var matches []string

	// Walk the sops directory
	err := filepath.Walk(sopsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}

		// Only check .md files
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".md") {
			filename := strings.ToLower(filepath.Base(path))
			relPath := strings.TrimPrefix(path, agentDir+string(filepath.Separator))

			// Check if any keyword matches the filename
			for _, keyword := range keywords {
				if strings.Contains(filename, strings.ToLower(keyword)) {
					matches = append(matches, relPath)
					break
				}
			}

			// Stop at 3 matches to prevent prompt bloat
			if len(matches) >= 3 {
				return filepath.SkipDir
			}
		}
		return nil
	})

	if err != nil {
		return nil
	}

	return matches
}

// readBoundedExcerpt reads a file and returns at most maxChars of its content,
// truncating on a line boundary and appending an ellipsis when content is cut.
// Deliberately independent of extractSection's 2000-char cap so SOP excerpts
// stay short enough to inline without bloating the prompt.
func readBoundedExcerpt(path string, maxChars int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	s := strings.TrimSpace(string(data))
	if len(s) <= maxChars {
		return s
	}

	cut := s[:maxChars]
	// Prefer a line boundary so we don't slice mid-line.
	if nl := strings.LastIndexByte(cut, '\n'); nl > 0 {
		cut = cut[:nl]
	}
	return strings.TrimRight(cut, " \n\t") + "\n…"
}

// extractTaskKeywords extracts relevant keywords from task description for SOP matching
func extractTaskKeywords(description string) []string {
	// Convert to lowercase for case-insensitive matching
	desc := strings.ToLower(description)

	// Common technical keywords to look for
	keywords := []string{
		"sqlite", "database", "db",
		"telegram", "slack", "github", "gitlab", "jira", "linear",
		"auth", "authentication", "oauth",
		"api", "webhook", "http", "rest", "graphql",
		"test", "testing", "unittest",
		"docker", "kubernetes", "k8s",
		"ci", "cd", "pipeline",
		"alert", "notification", "email",
		"tui", "dashboard", "ui",
		"debug", "debugging", "error",
		"integration", "adapter", "client",
	}

	var found []string
	for _, keyword := range keywords {
		if strings.Contains(desc, keyword) {
			found = append(found, keyword)
		}
	}

	return found
}

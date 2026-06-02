package architect

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// titleHasConventionalPrefix matches a title that already carries a
// conventional-commit type prefix (with optional scope). When a proposal's Title
// is already conventional, proposalTitle uses it verbatim; otherwise it is
// wrapped in a refactor(scope): prefix derived from the proposal's files.
var titleHasConventionalPrefix = regexp.MustCompile(`^(feat|fix|chore|refactor|test|docs|perf|build|ci|style)(\([^)]+\))?: .+$`)

// proposalTitle builds a conventional-commit issue title for a proposal. If the
// proposal already phrased its Title as a conventional commit, it is used as-is
// (after trimming). Otherwise the title becomes `refactor(<scope>): <title>`,
// where scope is derived from the proposal's first file (its top-level package
// or directory) and falls back to "architect" when no file is available. This
// guarantees github.CreatePilotIssue's conventional-commit validation passes.
func proposalTitle(f pilotapi.Finding) string {
	title := strings.TrimSpace(f.Title)
	if titleHasConventionalPrefix.MatchString(title) {
		return title
	}
	scope := scopeFromFiles(f.Files)
	return fmt.Sprintf("refactor(%s): %s", scope, title)
}

// scopeFromFiles derives a short conventional-commit scope from a proposal's
// files. It uses the first non-empty file's leading path segment (or, for a
// top-level file, the filename without extension), sanitised to the characters a
// scope may contain. It returns "architect" when no usable file is present.
func scopeFromFiles(files []string) string {
	for _, raw := range files {
		f := strings.TrimSpace(raw)
		if f == "" {
			continue
		}
		f = strings.ReplaceAll(f, "\\", "/")
		seg := f
		if i := strings.IndexByte(f, '/'); i > 0 {
			seg = f[:i]
		} else if dot := strings.LastIndexByte(f, '.'); dot > 0 {
			seg = f[:dot]
		}
		if s := sanitizeScope(seg); s != "" {
			return s
		}
	}
	return "architect"
}

// sanitizeScope keeps only characters valid in a conventional-commit scope
// (letters, digits, '-', '_', '.', '/') and trims separators from the ends. An
// all-invalid input yields the empty string so the caller can fall back.
func sanitizeScope(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == '/':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_./")
}

// proposalBody renders the markdown issue body for a proposal and appends the
// hidden dedup marker as the final line. The body is structured so a human (or
// the Pilot daemon) can act on it directly: why it matters, the suggested
// PR-sized pieces, the test plan, and the files in scope. Empty sections are
// omitted rather than rendered as blank headings.
func proposalBody(f pilotapi.Finding, marker string) string {
	var b strings.Builder

	b.WriteString("> Filed by the Pilot Architect (proactive refactor analysis).\n\n")
	fmt.Fprintf(&b, "**Risk:** %s\n", riskLabel(f.Risk))
	if kind := strings.TrimSpace(f.Kind); kind != "" {
		fmt.Fprintf(&b, "**Kind:** %s\n", kind)
	}
	b.WriteString("\n")

	writeSection(&b, "Why it matters", strings.TrimSpace(f.WhyItMatters))
	writeList(&b, "Suggested PR pieces", f.SuggestedPRPieces)
	writeSection(&b, "Test plan", strings.TrimSpace(f.TestPlan))
	writeFiles(&b, f.Files)

	b.WriteString("\n")
	b.WriteString(marker)
	b.WriteString("\n")
	return b.String()
}

// riskLabel returns a non-empty risk string for display, defaulting an empty
// risk to medium so the rendered body always shows a level.
func riskLabel(r pilotapi.RiskLevel) string {
	if r == "" {
		return string(pilotapi.RiskMedium)
	}
	return string(r)
}

// writeSection appends a "## title" heading and body, only when body is
// non-empty.
func writeSection(b *strings.Builder, title, body string) {
	if body == "" {
		return
	}
	fmt.Fprintf(b, "## %s\n\n%s\n\n", title, body)
}

// writeList appends a "## title" heading and a markdown bullet list, only when
// items contains at least one non-blank entry.
func writeList(b *strings.Builder, title string, items []string) {
	cleaned := make([]string, 0, len(items))
	for _, it := range items {
		if t := strings.TrimSpace(it); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, it := range cleaned {
		fmt.Fprintf(b, "- %s\n", it)
	}
	b.WriteString("\n")
}

// writeFiles appends a "## Files" heading with each file as inline-code in a
// bullet list, only when at least one non-blank file is present.
func writeFiles(b *strings.Builder, files []string) {
	cleaned := make([]string, 0, len(files))
	for _, f := range files {
		if t := strings.TrimSpace(f); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	b.WriteString("## Files\n\n")
	for _, f := range cleaned {
		fmt.Fprintf(b, "- `%s`\n", f)
	}
	b.WriteString("\n")
}

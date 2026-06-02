package dashboard

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// FetchGitGraph runs git fetch --prune then git log --graph, returns parsed state.
// Called from a background goroutine via tea.Cmd.
func FetchGitGraph(projectPath string, limit int) *GitGraphState {
	state := &GitGraphState{LastRefresh: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// git fetch --prune (best-effort; ignore errors so offline still works)
	fetchCmd := exec.CommandContext(ctx, "git", "-C", projectPath, "fetch", "--prune")
	if err := fetchCmd.Run(); err != nil {
		slog.Debug("git fetch --prune failed (non-fatal)", slog.String("err", err.Error()))
	}

	// git log --graph with custom format: sha|author|refs|message
	format := "%H|%aN|%D|%s"
	logCmd := exec.CommandContext(ctx, "git", "-C", projectPath,
		"log", "--graph", "--all", "--oneline",
		"--decorate=full",
		fmt.Sprintf("--pretty=format:%%x00%s", format),
		fmt.Sprintf("-n%d", limit),
	)
	out, err := logCmd.Output()
	if err != nil {
		state.Error = err.Error()
		return state
	}

	state.Lines = ParseGitGraphOutput(string(out))
	state.TotalCount = CountGitCommits(projectPath)
	return state
}

// CountGitCommits returns the total number of commits in the repo (best-effort).
func CountGitCommits(projectPath string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", projectPath, "rev-list", "--count", "--all")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n
}

// ParseGitGraphOutput splits raw git log output into structured GitGraphLine entries.
// Git outputs each commit as: <graph_chars>\x00sha|author|refs|message
// Non-commit graph lines (pure branch connectors) get only GraphChars set.
func ParseGitGraphOutput(raw string) []GitGraphLine {
	var lines []GitGraphLine

	for _, rawLine := range strings.Split(raw, "\n") {
		if rawLine == "" {
			continue
		}

		// Does this line contain the null-byte separator we injected?
		nulIdx := strings.IndexByte(rawLine, 0x00)
		if nulIdx < 0 {
			// Pure graph connector line (│, ├╌╮, etc.)
			gl := GitGraphLine{
				GraphChars: TranslateGraphChars(rawLine),
			}
			lines = append(lines, gl)
			continue
		}

		graphPart := rawLine[:nulIdx]
		dataPart := rawLine[nulIdx+1:]

		parts := strings.SplitN(dataPart, "|", 4)
		gl := GitGraphLine{
			GraphChars: TranslateGraphChars(graphPart),
		}
		if len(parts) >= 1 {
			sha := strings.TrimSpace(parts[0])
			if len(sha) >= 7 {
				gl.SHA = sha[:7]
			} else {
				gl.SHA = sha
			}
		}
		if len(parts) >= 2 {
			gl.Author = AbbreviateAuthor(strings.TrimSpace(parts[1]))
		}
		if len(parts) >= 3 {
			gl.Refs = strings.TrimSpace(parts[2])
		}
		if len(parts) >= 4 {
			gl.Message = strings.TrimSpace(parts[3])
		}

		lines = append(lines, gl)
	}
	return lines
}

// TranslateGraphChars replaces standard git graph characters with our design set.
//
// Git outputs: * | \ / -
// Our set:     ● │ ╮ ╯ ╌
//
// The tricky part is branch-off and merge patterns:
//
//	git: |\ → our: ├╌╮
//	git: |/ → our: ├╌╯
//	git: |_ → our: ├╌╌
func TranslateGraphChars(s string) string {
	// Replace git graph chars with our aesthetic set.
	// Process character by character to handle multi-byte sequences.
	var b strings.Builder
	runes := []rune(s)
	n := len(runes)
	for i := 0; i < n; i++ {
		r := runes[i]
		switch r {
		case '*':
			b.WriteRune('●')
		case '|':
			// Check if next non-space char is '\' or '/'
			b.WriteRune('│')
		case '\\':
			// Branch off: replace with ╮, but prefix needs ╌
			// The preceding '|' became '│'; we need ├╌╮ pattern.
			// Simpler: just output ╮ and handle ├ separately in colorize.
			b.WriteRune('╮')
		case '/':
			b.WriteRune('╯')
		case '-':
			b.WriteRune('╌')
		case '_':
			b.WriteRune('╌')
		default:
			b.WriteRune(r)
		}
	}

	// Post-process: fix junction characters.
	// Git outputs "├" as "|" adjacent to "\" — we need to replace
	// "│╮" with "├╌╮" and "│╯" with "├╌╯".
	result := b.String()
	result = strings.ReplaceAll(result, "│╮", "├╌╮")
	result = strings.ReplaceAll(result, "│╯", "├╌╯")
	result = strings.ReplaceAll(result, "│╌", "├╌")

	return result
}

// AbbreviateAuthor shortens an author name to fit in the full-mode column.
// "Firstname Lastname" → "F. Lastname" if > 10 chars.
func AbbreviateAuthor(name string) string {
	if len(name) <= 10 {
		return name
	}
	parts := strings.Fields(name)
	if len(parts) >= 2 {
		return string([]rune(parts[0])[0:1]) + ". " + parts[len(parts)-1]
	}
	if len(name) > 10 {
		return name[:10]
	}
	return name
}

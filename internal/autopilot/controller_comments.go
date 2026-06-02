package autopilot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// iterationRe matches the iteration field in autopilot-meta comments.
var iterationRe = regexp.MustCompile(`<!-- autopilot-meta.*?iteration:(\d+).*?-->`)

// buildMergeCompletionComment creates a success comment to post on an issue
// after its associated PR is merged. This ensures the last comment on the issue
// is a success message rather than a stale failure comment from a prior attempt.
func buildMergeCompletionComment(prState *PRState) string {
	var sb strings.Builder
	sb.WriteString("✅ PR merged successfully!\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| PR | #%d |\n", prState.PRNumber))
	sb.WriteString(fmt.Sprintf("| Branch | `%s` |\n", prState.BranchName))
	if !prState.CreatedAt.IsZero() {
		duration := time.Since(prState.CreatedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("| Time to merge | %s |\n", duration))
	}
	return sb.String()
}

// parseAutopilotIteration extracts the CI fix iteration counter from an issue body.
// Returns 0 if no iteration metadata is found (i.e., the issue is not a fix issue).
func parseAutopilotIteration(body string) int {
	if m := iterationRe.FindStringSubmatch(body); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

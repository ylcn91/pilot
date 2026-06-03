package architect

import (
	"fmt"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

const refactorPlanTestPlan = "go build ./... && go test ./..."

// FindingsFromRefactorPlan projects an ordered RefactorPlan into issue-emitter
// findings without losing handoff lineage. The returned slice preserves PR order;
// callers that care about the sequence should pass it through Emitter.EmitOrdered.
func FindingsFromRefactorPlan(plan RefactorPlan) []pilotapi.Finding {
	out := make([]pilotapi.Finding, 0, len(plan.PRs))
	for _, pr := range plan.PRs {
		out = append(out, pilotapi.Finding{
			Title:             pr.Title,
			Kind:              "refactor",
			Risk:              riskOrMedium(pr.Risk),
			WhyItMatters:      refactorPlanFindingBody(pr),
			SuggestedPRPieces: refactorPlanPieces(pr),
			TestPlan:          refactorPlanTestPlan,
			Files:             append([]string(nil), pr.Files...),
		})
	}
	return out
}

func refactorPlanFindingBody(pr PlannedPR) string {
	body := subtaskDescription(pr)
	if body == "" {
		return fmt.Sprintf("Order: PR %d", pr.Order)
	}
	return fmt.Sprintf("Order: PR %d\n%s", pr.Order, body)
}

func refactorPlanPieces(pr PlannedPR) []string {
	pieces := []string{fmt.Sprintf("Implement PR %d in the ordered refactor sequence", pr.Order)}
	if len(pr.DependsOn) > 0 {
		parts := make([]string, len(pr.DependsOn))
		for i, dep := range pr.DependsOn {
			parts[i] = fmt.Sprintf("PR %d", dep)
		}
		pieces = append(pieces, "Land after "+strings.Join(parts, ", "))
	}
	if !pr.PilotSafe {
		pieces = append(pieces, "Hold for manual review before unattended execution")
	}
	return pieces
}

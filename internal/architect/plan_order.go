package architect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ylcn91/pilot/internal/executor"
)

// epicParentTitle is the title of the synthetic parent task ProjectToEpic emits
// so the projected EpicPlan is self-describing when fed to the sub-issue
// machinery.
const epicParentTitle = "Refactor epic: ordered small-PR sequence"

// sortPlannedPRs orders the un-numbered PRs so leaves (low blast radius) come
// first and dependents follow. Ordering key, all ascending unless noted:
//  1. blast radius ascending — a change that ripples into fewer packages is a
//     safer leaf and lands first;
//  2. pilot-safe before manual — unattended-shippable work leads;
//  3. risk descending — among equal-blast leaves, do the higher-risk fix first;
//  4. title ascending — final deterministic tiebreak.
//
// The sort is stable so equal-key PRs retain their incoming (rank) order.
func sortPlannedPRs(prs []PlannedPR) {
	sort.SliceStable(prs, func(i, j int) bool {
		a, b := prs[i], prs[j]
		if a.BlastRadius != b.BlastRadius {
			return a.BlastRadius < b.BlastRadius
		}
		if a.PilotSafe != b.PilotSafe {
			return a.PilotSafe // true (safe) sorts before false (manual)
		}
		if ra, rb := riskRank(a.Risk), riskRank(b.Risk); ra != rb {
			return ra > rb
		}
		return a.Title < b.Title
	})
}

// capPlannedPRs enforces the [minPlanSubtasks, maxPlanSubtasks] envelope. A plan
// past the max is truncated to the highest-priority leaves (the slice is already
// sorted leaf-first). A plan below the min is left as-is — fewer real PRs is the
// honest answer; the lens decides whether so small a plan is worth an epic.
func capPlannedPRs(prs []PlannedPR) []PlannedPR {
	if len(prs) > maxPlanSubtasks {
		return prs[:maxPlanSubtasks]
	}
	return prs
}

// assignOrderAndDeps fills each PR's 1-indexed Order and its DependsOn edges from
// the now-sorted slice. A PR depends on the immediately-preceding PR only when
// that predecessor has a strictly lower blast radius, encoding the leaf-before-
// dependent chain without fabricating a dense, unreviewable dependency mesh:
// equal-blast siblings stay independent (parallelisable), while each step up in
// blast radius is gated behind the lower tier landing first.
func assignOrderAndDeps(prs []PlannedPR) {
	for i := range prs {
		prs[i].Order = i + 1
		prs[i].DependsOn = nil
		if i > 0 && prs[i-1].BlastRadius < prs[i].BlastRadius {
			prs[i].DependsOn = []int{prs[i-1].Order}
		}
	}
}

// projectEpic maps the ordered PlannedPR slice onto the executor's EpicPlan so
// the sequence can feed createSubIssuesViaAdapter / ExecuteSubIssues unchanged.
// Each PR becomes one PlannedSubtask carrying the same Order/DependsOn; the
// subtask Description embeds the class, blast radius, pilot-safety, and owner so
// the downstream ticket body is self-contained. An empty plan yields a non-nil
// EpicPlan with no subtasks.
func projectEpic(prs []PlannedPR) *executor.EpicPlan {
	subs := make([]executor.PlannedSubtask, 0, len(prs))
	for _, pr := range prs {
		subs = append(subs, executor.PlannedSubtask{
			Title:       pr.Title,
			Description: subtaskDescription(pr),
			Order:       pr.Order,
			DependsOn:   append([]int(nil), pr.DependsOn...),
		})
	}
	return &executor.EpicPlan{
		ParentTask: &executor.Task{Title: epicParentTitle},
		Subtasks:   subs,
	}
}

// subtaskDescription renders the self-contained ticket body for one planned PR:
// its classification, blast radius, the pilot-safe / manual verdict (with the
// reason when manual), the touched files, and the best-effort owner. Kept
// deterministic so identical plans produce byte-identical ticket bodies.
func subtaskDescription(pr PlannedPR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Class: %s\n", pr.Class)
	fmt.Fprintf(&b, "Blast radius: %d dependent package(s)\n", pr.BlastRadius)
	if pr.PilotSafe {
		b.WriteString("Execution: pilot-safe (may run unattended)\n")
	} else {
		fmt.Fprintf(&b, "Execution: MANUAL REVIEW REQUIRED — %s\n", pr.ManualReason)
	}
	if pr.Owner != "" {
		fmt.Fprintf(&b, "Suggested reviewer (top author): %s\n", pr.Owner)
	}
	if len(pr.Files) > 0 {
		fmt.Fprintf(&b, "Files: %s\n", strings.Join(pr.Files, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

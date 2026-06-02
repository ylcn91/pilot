package executor

import (
	"context"
	"fmt"
)

// commitProbeFunc reports the current commit count on the working branch
// relative to the TDD base. enforceTDDImplementerCommit uses it to detect
// whether the IMPLEMENTER landed a fresh commit beyond the test-author baseline.
type commitProbeFunc func(ctx context.Context) (int, error)

// tddImplementerCommitFeedback is the explicit instruction handed to a
// re-prompted IMPLEMENTER that made the tests pass without committing.
const tddImplementerCommitFeedback = "Your implementation makes the tests pass but you did NOT commit it. " +
	"COMMIT your implementation now (git add + git commit) so it lands on the branch; " +
	"the working-tree changes alone are not enough and will be lost."

// enforceTDDImplementerCommit asserts the IMPLEMENTER committed its work. The
// GREEN gate judges the WORKING TREE, so an implementer that makes tests pass
// without committing yields a PR carrying only the test-author commit (the
// implementation is lost). This guard requires the commit count to grow past
// baseline (the test-author's count). When it has not, it re-prompts the
// IMPLEMENTER — bounded by greenMax — with explicit feedback to COMMIT, then
// re-verifies BOTH a fresh commit AND that GREEN still holds. If no implementer
// commit lands within the budget, the run FAILS with reasonTDDImplementerNoCommit
// (we never silently finalize a tests-only PR). It mirrors the TEST-AUTHOR
// no-commit enforcement (runTDDTestAuthor) which already errors after retries.
func (r *Runner) enforceTDDImplementerCommit(
	ctx context.Context,
	taskID, projectPath string,
	testNames []string,
	scopeToNew bool,
	baseline, greenMax int,
	commitCount commitProbeFunc,
	rerunImplementer roleRerunFunc,
) error {
	if greenMax < 0 {
		greenMax = defaultTDDGreenMaxRetries
	}
	for attempt := 0; ; attempt++ {
		count, err := commitCount(ctx)
		if err != nil {
			return err
		}
		if count > baseline {
			return nil // implementer committed — implementation is captured
		}
		if attempt >= greenMax {
			return fmt.Errorf("%s: tests pass in the working tree but the IMPLEMENTER landed no commit after %d retr%s — refusing a tests-only PR",
				reasonTDDImplementerNoCommit, greenMax, plural(greenMax))
		}
		if rerunImplementer == nil {
			return fmt.Errorf("%s: tests pass but the IMPLEMENTER landed no commit and no rerun is available", reasonTDDImplementerNoCommit)
		}
		if rErr := rerunImplementer(ctx, tddImplementerCommitFeedback); rErr != nil {
			return rErr
		}
		// The re-prompted implementer may have changed the tree; re-assert GREEN so
		// a "commit" that breaks the tests cannot satisfy this guard.
		greenOK, feedback, gerr := r.runTDDGreenGate(ctx, taskID, projectPath, testNames, scopeToNew)
		if gerr != nil {
			return gerr
		}
		if !greenOK {
			return fmt.Errorf("%s: tests regressed after re-prompting the IMPLEMENTER to commit: %s", reasonTDDGreenGateFailed, feedback)
		}
	}
}

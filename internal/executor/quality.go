package executor

import (
	"context"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// QualityGateDetail is the shared gate-detail contract, defined in the leaf
// pilotapi package. Aliased here so existing in-package references keep working.
type QualityGateDetail = pilotapi.QualityGateDetail

// QualityOutcome is the shared quality-run contract, defined in the leaf
// pilotapi package. Aliased here so existing in-package references keep working.
type QualityOutcome = pilotapi.QualityOutcome

// QualityChecker is an interface for running quality gate checks.
// This interface allows the executor to run quality gates without
// importing the quality package directly, avoiding import cycles.
type QualityChecker interface {
	// Check runs all quality gates and returns the outcome
	Check(ctx context.Context) (*QualityOutcome, error)
}

// QualityCheckerFactory creates a QualityChecker for a specific task.
// This allows the runner to create quality checkers on demand with
// the correct task context without knowing about the quality package.
// The factory is typically implemented in main.go where both packages
// can be imported.
type QualityCheckerFactory func(taskID, projectPath string) QualityChecker

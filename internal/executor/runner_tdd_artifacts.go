package executor

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// recordTDDArtifact appends a typed, traceable handoff artifact for one TDD role
// to s.tddArtifacts, chaining it to the prior artifact via ParentHash. When a
// pipeline plan artifact exists it becomes the parent of the first TDD artifact,
// yielding plan -> architect -> test-author -> implementer. Without a plan, the
// architect remains the chain root.
//
// It returns the new artifact's TraceHash so callers may chain explicitly, but
// the stored chain is self-linking: each call reads the previous tail's hash.
func (r *Runner) recordTDDArtifact(s *executeState, role, content string) string {
	parent := tddChainParent(s)
	art := pilotapi.NewHandoffArtifact(role, s.task.ID, content, parent)
	s.tddArtifacts = append(s.tddArtifacts, art)

	r.log.Debug("TDD handoff artifact recorded",
		slog.String("task_id", s.task.ID),
		slog.String("role", art.Role),
		slog.String("trace_hash", art.TraceHash),
		slog.String("parent_hash", art.ParentHash),
		slog.Int("chain_len", len(s.tddArtifacts)),
	)
	return art.TraceHash
}

// tddChainParent returns the ParentHash to assign to the next artifact in the
// chain: the TraceHash of the TDD tail, the plan artifact's TraceHash when TDD
// is still empty, or "" when neither exists.
func tddChainParent(s *executeState) string {
	if len(s.tddArtifacts) > 0 {
		return s.tddArtifacts[len(s.tddArtifacts)-1].TraceHash
	}
	if s.planArtifact.TraceHash != "" {
		return s.planArtifact.TraceHash
	}
	return ""
}

// tddArtifactByRole returns the first recorded TDD artifact for role. A zero
// artifact means the role has not produced a typed handoff yet.
func tddArtifactByRole(chain []pilotapi.HandoffArtifact, role string) pilotapi.HandoffArtifact {
	for _, art := range chain {
		if art.Role == role {
			return art
		}
	}
	return pilotapi.HandoffArtifact{}
}

// tddArtifactChain returns the combined plan/TDD chain in execution order.
func tddArtifactChain(s *executeState) []pilotapi.HandoffArtifact {
	chain := make([]pilotapi.HandoffArtifact, 0, len(s.tddArtifacts)+1)
	if s.planArtifact.TraceHash != "" {
		chain = append(chain, s.planArtifact)
	}
	chain = append(chain, s.tddArtifacts...)
	return chain
}

package executor

import (
	"log/slog"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// recordTDDArtifact appends a typed, traceable handoff artifact for one TDD role
// to s.tddArtifacts, chaining it to the prior role via ParentHash. The parent is
// the TraceHash of the last recorded artifact (empty for the first role, making
// the architect the chain root). The prose appendices are what role backends
// consume; this artifact is the versioned, content-addressable audit record that
// links architect -> test-author -> implementer into an auditable lineage.
//
// It returns the new artifact's TraceHash so callers may chain explicitly, but
// the stored chain is self-linking: each call reads the previous tail's hash.
func (r *Runner) recordTDDArtifact(s *executeState, role, content string) string {
	parent := tddChainParent(s.tddArtifacts)
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
// chain: the TraceHash of the current tail, or "" when the chain is empty (the
// first role becomes the chain root).
func tddChainParent(chain []pilotapi.HandoffArtifact) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].TraceHash
}

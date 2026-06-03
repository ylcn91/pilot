package executor

import (
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// lineageRecorderStub implements KnowledgeGraphRecorder plus the AddLearning
// capability recordHandoffLineage type-asserts for. It captures every learning
// node written so tests can assert node count and metadata lineage.
type lineageRecorderStub struct {
	learnings []learningNode
	addErr    error
}

type learningNode struct {
	title    string
	content  string
	metadata map[string]interface{}
}

func (s *lineageRecorderStub) AddLearning(title, content string, metadata map[string]interface{}) error {
	s.learnings = append(s.learnings, learningNode{title: title, content: content, metadata: metadata})
	return s.addErr
}

func (s *lineageRecorderStub) AddExecutionLearning(_, _ string, _ []string, _ []string, _ string) error {
	return nil
}

func (s *lineageRecorderStub) GetRelatedByKeywords(_ []string) []*memory.GraphNode { return nil }

// chainState builds an executeState whose plan/TDD artifacts form a parent-linked
// chain plan -> architect -> test-author -> implementer, omitting the plan stage
// when withPlan is false (so the chain starts at the architect).
func chainState(taskID string, withPlan bool) *executeState {
	s := &executeState{task: &Task{ID: taskID}}
	parent := ""
	if withPlan {
		s.planArtifact = pilotapi.NewHandoffArtifact(pilotapi.RolePlan, taskID, "PLAN", "")
		parent = s.planArtifact.TraceHash
	}
	for _, role := range []string{pilotapi.RoleArchitect, pilotapi.RoleTestAuthor, pilotapi.RoleImplementer} {
		art := pilotapi.NewHandoffArtifact(role, taskID, "content:"+role, parent)
		s.tddArtifacts = append(s.tddArtifacts, art)
		parent = art.TraceHash
	}
	return s
}

func TestRecordHandoffLineage(t *testing.T) {
	tests := []struct {
		name       string
		state      *executeState
		wantNodes  int
		wantRoles  []string
		wantHashes []string // expected trace hashes in write order, for chain assertion
	}{
		{
			name:      "plan plus tdd chain writes one node per artifact",
			state:     chainState("GH-1", true),
			wantNodes: 4,
			wantRoles: []string{
				pilotapi.RolePlan,
				pilotapi.RoleArchitect,
				pilotapi.RoleTestAuthor,
				pilotapi.RoleImplementer,
			},
		},
		{
			name:      "tdd-only chain (no plan stage) starts at architect",
			state:     chainState("GH-2", false),
			wantNodes: 3,
			wantRoles: []string{
				pilotapi.RoleArchitect,
				pilotapi.RoleTestAuthor,
				pilotapi.RoleImplementer,
			},
		},
		{
			name:      "empty chain writes nothing",
			state:     &executeState{task: &Task{ID: "GH-3"}},
			wantNodes: 0,
		},
		{
			name: "plan stage with empty trace hash is skipped",
			state: func() *executeState {
				s := chainState("GH-4", false)
				// planArtifact left as zero value (TraceHash == "") => skipped.
				return s
			}(),
			wantNodes: 3,
			wantRoles: []string{
				pilotapi.RoleArchitect,
				pilotapi.RoleTestAuthor,
				pilotapi.RoleImplementer,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRunner()
			sink := &lineageRecorderStub{}
			r.SetKnowledgeGraph(sink)

			r.recordHandoffLineage(tt.state)

			if len(sink.learnings) != tt.wantNodes {
				t.Fatalf("wrote %d nodes, want %d", len(sink.learnings), tt.wantNodes)
			}

			// Build the expected source chain to assert metadata against.
			var chain []pilotapi.HandoffArtifact
			if tt.state.planArtifact.TraceHash != "" {
				chain = append(chain, tt.state.planArtifact)
			}
			chain = append(chain, tt.state.tddArtifacts...)

			for i, node := range sink.learnings {
				art := chain[i]

				if node.title != "handoff:"+art.Role {
					t.Errorf("node[%d] title = %q, want %q", i, node.title, "handoff:"+art.Role)
				}
				if got := node.metadata["role"]; got != art.Role {
					t.Errorf("node[%d] role = %v, want %q", i, got, art.Role)
				}
				if got := node.metadata["trace_hash"]; got != art.TraceHash {
					t.Errorf("node[%d] trace_hash = %v, want %q", i, got, art.TraceHash)
				}
				if got := node.metadata["parent_hash"]; got != art.ParentHash {
					t.Errorf("node[%d] parent_hash = %v, want %q", i, got, art.ParentHash)
				}
				if got := node.metadata["task_id"]; got != art.TaskID {
					t.Errorf("node[%d] task_id = %v, want %q", i, got, art.TaskID)
				}
				if got := node.metadata["schema_version"]; got != art.SchemaVersion {
					t.Errorf("node[%d] schema_version = %v, want %d", i, got, art.SchemaVersion)
				}

				// Chain integrity: each node's parent_hash is the prior node's trace_hash.
				if i == 0 {
					if got := node.metadata["parent_hash"]; got != "" {
						t.Errorf("first node parent_hash = %v, want empty (chain root)", got)
					}
				} else {
					prev := sink.learnings[i-1].metadata["trace_hash"]
					if got := node.metadata["parent_hash"]; got != prev {
						t.Errorf("node[%d] parent_hash = %v, want prior trace_hash %v", i, got, prev)
					}
				}
			}

			if tt.wantRoles != nil {
				gotRoles := make([]string, len(sink.learnings))
				for i, node := range sink.learnings {
					gotRoles[i] = node.metadata["role"].(string)
				}
				if strings.Join(gotRoles, ",") != strings.Join(tt.wantRoles, ",") {
					t.Errorf("roles = %v, want %v", gotRoles, tt.wantRoles)
				}
			}
		})
	}
}

// TestRecordHandoffLineageNoSink covers the guard paths: a nil knowledge graph
// and a recorder that lacks AddLearning are both no-ops (no panic, no write).
func TestRecordHandoffLineageNoSink(t *testing.T) {
	t.Run("nil knowledge graph", func(t *testing.T) {
		r := NewRunner()
		r.recordHandoffLineage(chainState("GH-nil", true)) // must not panic
	})

	t.Run("recorder without AddLearning is skipped", func(t *testing.T) {
		r := NewRunner()
		r.SetKnowledgeGraph(&mockKnowledgeGraphRecorder{})
		r.recordHandoffLineage(chainState("GH-noassert", true)) // must not panic
	})
}

// TestRecordHandoffLineageWriteErrorNonFatal verifies an AddLearning error is
// swallowed: every artifact is still attempted and the method returns normally.
func TestRecordHandoffLineageWriteErrorNonFatal(t *testing.T) {
	r := NewRunner()
	sink := &lineageRecorderStub{addErr: errors.New("graph write failed")}
	r.SetKnowledgeGraph(sink)

	r.recordHandoffLineage(chainState("GH-err", true))

	if len(sink.learnings) != 4 {
		t.Fatalf("attempted %d writes, want 4 (errors are non-fatal, all artifacts attempted)", len(sink.learnings))
	}
}

// TestRecordHandoffLineageTruncatesContent verifies the ~500-char truncation
// idiom is applied to artifact content before it is written to the graph.
func TestRecordHandoffLineageTruncatesContent(t *testing.T) {
	r := NewRunner()
	sink := &lineageRecorderStub{}
	r.SetKnowledgeGraph(sink)

	long := strings.Repeat("x", 800)
	s := &executeState{task: &Task{ID: "GH-trunc"}}
	s.tddArtifacts = []pilotapi.HandoffArtifact{
		pilotapi.NewHandoffArtifact(pilotapi.RoleImplementer, "GH-trunc", long, ""),
	}

	r.recordHandoffLineage(s)

	if len(sink.learnings) != 1 {
		t.Fatalf("wrote %d nodes, want 1", len(sink.learnings))
	}
	if got := len(sink.learnings[0].content); got != 500 {
		t.Errorf("content length = %d, want 500 (truncated)", got)
	}
}

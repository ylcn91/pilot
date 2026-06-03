package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// TestRunTDDSequenceRecordsChainedArtifacts verifies the typed handoff record:
// a full happy-path TDD run stores exactly three artifacts in role order
// (architect -> test-author -> implementer), each chained by ParentHash to the
// prior role's TraceHash, with the architect as the chain root.
func TestRunTDDSequenceRecordsChainedArtifacts(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})
	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect", output: "DESIGN: add func"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	if _, err := r.runTDDSequence(s); err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}

	arts := s.tddArtifacts
	if len(arts) != 3 {
		t.Fatalf("recorded %d artifacts, want 3 (architect/test-author/implementer)", len(arts))
	}

	wantRoles := []string{pilotapi.RoleArchitect, pilotapi.RoleTestAuthor, pilotapi.RoleImplementer}
	for i, want := range wantRoles {
		if arts[i].Role != want {
			t.Errorf("artifact[%d].Role = %q, want %q", i, arts[i].Role, want)
		}
		if arts[i].TaskID != s.task.ID {
			t.Errorf("artifact[%d].TaskID = %q, want %q", i, arts[i].TaskID, s.task.ID)
		}
		if arts[i].SchemaVersion != pilotapi.SchemaVersion {
			t.Errorf("artifact[%d].SchemaVersion = %d, want %d", i, arts[i].SchemaVersion, pilotapi.SchemaVersion)
		}
		if arts[i].TraceHash == "" {
			t.Errorf("artifact[%d].TraceHash empty", i)
		}
	}

	// Chain integrity: architect is the root, each later role's ParentHash is the
	// prior role's TraceHash.
	if arts[0].ParentHash != "" {
		t.Errorf("architect ParentHash = %q, want empty (chain root)", arts[0].ParentHash)
	}
	if arts[1].ParentHash != arts[0].TraceHash {
		t.Errorf("test-author ParentHash = %q, want architect TraceHash %q", arts[1].ParentHash, arts[0].TraceHash)
	}
	if arts[2].ParentHash != arts[1].TraceHash {
		t.Errorf("implementer ParentHash = %q, want test-author TraceHash %q", arts[2].ParentHash, arts[1].TraceHash)
	}

	// Content carries each role's contribution.
	if arts[0].Content != "DESIGN: add func" {
		t.Errorf("architect content = %q, want captured design", arts[0].Content)
	}
	if !strings.Contains(arts[1].Content, "TestAdd") {
		t.Errorf("test-author content = %q, want authored test name", arts[1].Content)
	}
	if arts[2].Content != "IMPLEMENTED" {
		t.Errorf("implementer content = %q, want implementer output", arts[2].Content)
	}

	// Each TraceHash matches a freshly reconstructed artifact (deterministic).
	for i, art := range arts {
		want := pilotapi.NewHandoffArtifact(art.Role, art.TaskID, art.Content, art.ParentHash)
		if art != want {
			t.Errorf("artifact[%d] = %+v, not reproducible by NewHandoffArtifact: %+v", i, art, want)
		}
	}
}

// TestRunTDDSequenceArtifactsDistinctHashes verifies the chained artifacts have
// distinct TraceHashes (no accidental collision across roles), so the lineage is
// unambiguous.
func TestRunTDDSequenceArtifactsDistinctHashes(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{fail2(), pass()})
	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect", output: "DESIGN"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: add failing test"),
	}
	r.implementerBackend = &tddRoleBackend{
		name: "impl", prompts: order, role: "implementer",
		output: "IMPLEMENTED",
		action: commitFile(t, dir, "add.go", "package tddtest\n", "feat: implement add"),
	}

	if _, err := r.runTDDSequence(s); err != nil {
		t.Fatalf("runTDDSequence: %v", err)
	}

	seen := map[string]string{}
	for _, art := range s.tddArtifacts {
		if prev, ok := seen[art.TraceHash]; ok {
			t.Fatalf("TraceHash collision %q between roles %q and %q", art.TraceHash, prev, art.Role)
		}
		seen[art.TraceHash] = art.Role
	}
}

// TestRunTDDSequenceAbortRecordsPartialChain verifies that when the RED gate
// never goes red (abort before the implementer), only the architect and
// test-author artifacts are recorded — and they still chain correctly — so the
// audit record reflects exactly the roles that ran.
func TestRunTDDSequenceAbortRecordsArchitectOnly(t *testing.T) {
	dir, defaultBranch := tddGitRepo(t)
	order := &[]string{}

	// RED gate always passes => tests never red => abort before recording
	// test-author/implementer in the post-gate sequence.
	r, s, _ := newTDDRunner(t, dir, defaultBranch, order, []*QualityOutcome{pass()})
	r.architectBackend = &tddRoleBackend{name: "arch", prompts: order, role: "architect", output: "DESIGN"}
	r.testAuthorBackend = &tddRoleBackend{
		name: "ta", prompts: order, role: "test-author",
		output: "TESTS_ADDED: TestAdd",
		action: commitFile(t, dir, "add_test.go", "package tddtest\n", "test: passing test"),
	}
	r.implementerBackend = &tddRoleBackend{name: "impl", prompts: order, role: "implementer"}

	if _, err := r.runTDDSequence(s); err == nil {
		t.Fatal("expected abort when RED gate never goes red")
	}

	if len(s.tddArtifacts) != 1 {
		t.Fatalf("recorded %d artifacts on RED abort, want 1 (architect only)", len(s.tddArtifacts))
	}
	if s.tddArtifacts[0].Role != pilotapi.RoleArchitect {
		t.Errorf("sole artifact role = %q, want architect", s.tddArtifacts[0].Role)
	}
	if s.tddArtifacts[0].ParentHash != "" {
		t.Errorf("architect ParentHash = %q, want empty (chain root)", s.tddArtifacts[0].ParentHash)
	}
}

// TestTDDArtifactsEmptyWhenDisabled verifies the chain is empty/safe when TDD is
// not driven: with no sequence run, s.tddArtifacts is nil and the helper readers
// tolerate it.
func TestTDDArtifactsEmptyWhenDisabled(t *testing.T) {
	s := &executeState{task: &Task{ID: "GH-disabled"}, ctx: context.Background()}
	if s.tddArtifacts != nil {
		t.Fatalf("tddArtifacts = %+v, want nil before any TDD run", s.tddArtifacts)
	}
	if got := tddChainParent(s); got != "" {
		t.Errorf("tddChainParent(nil) = %q, want empty", got)
	}
}

// TestRecordTDDArtifactChainsSelfLinking unit-tests the recorder in isolation:
// successive recordings form a parent-linked chain rooted at the first call,
// and the returned hash equals the stored tail's TraceHash.
func TestRecordTDDArtifactChainsSelfLinking(t *testing.T) {
	r := NewRunner()
	s := &executeState{task: &Task{ID: "GH-rec"}}

	h1 := r.recordTDDArtifact(s, pilotapi.RoleArchitect, "design")
	h2 := r.recordTDDArtifact(s, pilotapi.RoleTestAuthor, "tests")
	h3 := r.recordTDDArtifact(s, pilotapi.RoleImplementer, "impl")

	if len(s.tddArtifacts) != 3 {
		t.Fatalf("chain length = %d, want 3", len(s.tddArtifacts))
	}
	if s.tddArtifacts[0].TraceHash != h1 || s.tddArtifacts[1].TraceHash != h2 || s.tddArtifacts[2].TraceHash != h3 {
		t.Fatal("returned hashes do not match stored tail TraceHashes")
	}
	if s.tddArtifacts[0].ParentHash != "" {
		t.Errorf("first artifact ParentHash = %q, want empty", s.tddArtifacts[0].ParentHash)
	}
	if s.tddArtifacts[1].ParentHash != h1 {
		t.Errorf("second ParentHash = %q, want %q", s.tddArtifacts[1].ParentHash, h1)
	}
	if s.tddArtifacts[2].ParentHash != h2 {
		t.Errorf("third ParentHash = %q, want %q", s.tddArtifacts[2].ParentHash, h2)
	}
}

// TestRecordTDDArtifactChainsAfterPlanArtifact covers the full plan -> TDD
// lineage: when the pipeline plan stage produced a typed artifact, the
// architect must point to the plan TraceHash instead of becoming a second root.
func TestRecordTDDArtifactChainsAfterPlanArtifact(t *testing.T) {
	r := NewRunner()
	s := &executeState{task: &Task{ID: "GH-plan-tdd"}}
	s.planArtifact = pilotapi.NewHandoffArtifact(pilotapi.RolePlan, s.task.ID, "plan", "")

	r.recordTDDArtifact(s, pilotapi.RoleArchitect, "design")
	r.recordTDDArtifact(s, pilotapi.RoleTestAuthor, "tests")
	r.recordTDDArtifact(s, pilotapi.RoleImplementer, "impl")

	if s.tddArtifacts[0].ParentHash != s.planArtifact.TraceHash {
		t.Fatalf("architect ParentHash = %q, want plan TraceHash %q",
			s.tddArtifacts[0].ParentHash, s.planArtifact.TraceHash)
	}
	if err := pilotapi.ValidateHandoffChain(tddArtifactChain(s)); err != nil {
		t.Fatalf("combined plan/TDD chain invalid: %v", err)
	}
}

// TestTDDArtifactContentHelpers covers the content renderers' edge cases: empty
// test-name list and nil implementer result both yield empty strings (artifacts
// still chain), and populated inputs render as expected.
func TestTDDArtifactContentHelpers(t *testing.T) {
	if got := tddTestAuthorArtifactContent(nil); got != "" {
		t.Errorf("tddTestAuthorArtifactContent(nil) = %q, want empty", got)
	}
	if got := tddTestAuthorArtifactContent([]string{"TestA", "TestB"}); got != "TestA\nTestB" {
		t.Errorf("tddTestAuthorArtifactContent = %q, want joined names", got)
	}
	if got := tddImplementerArtifactContent(nil); got != "" {
		t.Errorf("tddImplementerArtifactContent(nil) = %q, want empty", got)
	}
	if got := tddImplementerArtifactContent(&BackendResult{Output: "done"}); got != "done" {
		t.Errorf("tddImplementerArtifactContent = %q, want result output", got)
	}
}

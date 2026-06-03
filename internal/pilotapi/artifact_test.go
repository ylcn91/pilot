package pilotapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewHandoffArtifactFillsFields(t *testing.T) {
	a := NewHandoffArtifact(RolePlan, "TASK-42", "spec body", "parent123abc")

	if a.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", a.SchemaVersion, SchemaVersion)
	}
	if a.Role != RolePlan {
		t.Errorf("Role = %q, want %q", a.Role, RolePlan)
	}
	if a.TaskID != "TASK-42" {
		t.Errorf("TaskID = %q, want %q", a.TaskID, "TASK-42")
	}
	if a.Content != "spec body" {
		t.Errorf("Content = %q, want %q", a.Content, "spec body")
	}
	if a.ParentHash != "parent123abc" {
		t.Errorf("ParentHash = %q, want %q", a.ParentHash, "parent123abc")
	}
	if a.TraceHash == "" {
		t.Fatal("TraceHash empty, want computed hash")
	}
	if !hex12.MatchString(a.TraceHash) {
		t.Errorf("TraceHash = %q, not 12 hex chars", a.TraceHash)
	}
}

func TestNewHandoffArtifactTraceHashMatchesTraceHash(t *testing.T) {
	a := NewHandoffArtifact(RoleArchitect, "TASK-9", "design", "p0")
	want := TraceHash(RoleArchitect, "TASK-9", "design", "p0")
	if a.TraceHash != want {
		t.Errorf("TraceHash = %q, want %q", a.TraceHash, want)
	}
}

func TestNewHandoffArtifactHashDependsOnEveryInput(t *testing.T) {
	base := NewHandoffArtifact(RolePlan, "TASK-1", "c", "p")
	variants := []HandoffArtifact{
		NewHandoffArtifact(RoleReview, "TASK-1", "c", "p"), // role differs
		NewHandoffArtifact(RolePlan, "TASK-2", "c", "p"),   // task differs
		NewHandoffArtifact(RolePlan, "TASK-1", "c2", "p"),  // content differs
		NewHandoffArtifact(RolePlan, "TASK-1", "c", "p2"),  // parent differs
	}
	for i, v := range variants {
		if v.TraceHash == base.TraceHash {
			t.Errorf("variant %d shares TraceHash with base %q; input change not reflected", i, base.TraceHash)
		}
	}
}

func TestNewHandoffArtifactDeterministic(t *testing.T) {
	a := NewHandoffArtifact(RoleImplementer, "TASK-3", "impl", "parentX")
	b := NewHandoffArtifact(RoleImplementer, "TASK-3", "impl", "parentX")
	if a != b {
		t.Errorf("identical inputs produced different artifacts:\n%+v\n%+v", a, b)
	}
}

func TestNewHandoffArtifactEmptyParent(t *testing.T) {
	a := NewHandoffArtifact(RolePlan, "TASK-0", "root spec", "")
	if a.ParentHash != "" {
		t.Errorf("ParentHash = %q, want empty (root artifact)", a.ParentHash)
	}
	if a.TraceHash == "" {
		t.Error("root artifact must still have a TraceHash")
	}
}

func TestHandoffArtifactJSONRoundTrip(t *testing.T) {
	orig := NewHandoffArtifact(RoleExecute, "TASK-100", "execute now", "parentHash00")

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got HandoffArtifact
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != orig {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, orig)
	}
}

func TestHandoffArtifactJSONFieldNames(t *testing.T) {
	a := NewHandoffArtifact(RolePlan, "TASK-1", "c", "p")
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	for _, key := range []string{"schema_version", "role", "content", "trace_hash", "parent_hash", "task_id"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in %s", key, data)
		}
	}
}

func TestValidateHandoffChainAcceptsContiguousChain(t *testing.T) {
	taskID := "TASK-chain"
	plan := NewHandoffArtifact(RolePlan, taskID, "plan", "")
	architect := NewHandoffArtifact(RoleArchitect, taskID, "design", plan.TraceHash)
	testAuthor := NewHandoffArtifact(RoleTestAuthor, taskID, "tests", architect.TraceHash)
	implementer := NewHandoffArtifact(RoleImplementer, taskID, "impl", testAuthor.TraceHash)

	if err := ValidateHandoffChain([]HandoffArtifact{plan, architect, testAuthor, implementer}); err != nil {
		t.Fatalf("ValidateHandoffChain: %v", err)
	}
}

func TestValidateHandoffChainRejectsBrokenParent(t *testing.T) {
	taskID := "TASK-broken-parent"
	plan := NewHandoffArtifact(RolePlan, taskID, "plan", "")
	architect := NewHandoffArtifact(RoleArchitect, taskID, "design", "wrong-parent")

	if err := ValidateHandoffChain([]HandoffArtifact{plan, architect}); err == nil {
		t.Fatal("expected broken parent hash error")
	}
}

func TestValidateHandoffChainRejectsMutatedTrace(t *testing.T) {
	art := NewHandoffArtifact(RolePlan, "TASK-mutated", "plan", "")
	art.Content = "mutated after hashing"

	if err := ValidateHandoffChain([]HandoffArtifact{art}); err == nil {
		t.Fatal("expected trace hash mismatch")
	}
}

func TestValidateHandoffChainRejectsSchemaMismatch(t *testing.T) {
	art := NewHandoffArtifact(RolePlan, "TASK-schema", "plan", "")
	art.SchemaVersion = SchemaVersion + 1

	if err := ValidateHandoffChain([]HandoffArtifact{art}); err == nil {
		t.Fatal("expected schema mismatch")
	}
}

func TestFindingJSONRoundTrip(t *testing.T) {
	orig := Finding{
		Title:             "Race in queue drain",
		Kind:              "bug",
		Risk:              RiskReleaseBlocker,
		WhyItMatters:      "drops tasks under load",
		SuggestedPRPieces: []string{"add mutex", "regression test"},
		TestPlan:          "stress test 1k concurrent enqueues",
		Files:             []string{"internal/queue/drain.go"},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Finding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, orig)
	}
	if !got.Risk.IsValid() {
		t.Errorf("round-tripped Risk %q is not valid", string(got.Risk))
	}
}

func TestFindingJSONFieldNames(t *testing.T) {
	data, err := json.Marshal(Finding{Risk: RiskLow})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	for _, key := range []string{
		"title", "kind", "risk", "why_it_matters",
		"suggested_pr_pieces", "test_plan", "files",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in %s", key, data)
		}
	}
	if m["risk"] != "low" {
		t.Errorf("risk serialized as %v, want \"low\"", m["risk"])
	}
}

func TestFindingZeroValueRoundTrip(t *testing.T) {
	var orig Finding
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Finding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("zero-value round trip mismatch:\n got %+v\nwant %+v", got, orig)
	}
}

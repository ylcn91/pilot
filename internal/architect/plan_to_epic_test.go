package architect

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

const planMod = "example.com/proj"

// planGraph builds a small fixture graph:
//
//	core  <- mid  <- top      (three packages importing down the chain)
//	leaf                       (no dependents)
//
// so a change to core has blast radius 2 (mid, top), mid has 1 (top), and both
// top and leaf have 0. This lets the ordering tests assert leaf-before-dependent.
func planGraph() *PackageGraph {
	return NewPackageGraph([]PackageNode{
		{ImportPath: planMod + "/internal/core", Module: planMod},
		{ImportPath: planMod + "/internal/mid", Module: planMod, Imports: []string{planMod + "/internal/core"}},
		{ImportPath: planMod + "/internal/top", Module: planMod, Imports: []string{planMod + "/internal/mid"}},
		{ImportPath: planMod + "/internal/leaf", Module: planMod},
	})
}

func findingTouching(title, kind string, risk pilotapi.RiskLevel, files ...string) pilotapi.Finding {
	return pilotapi.Finding{Title: title, Kind: kind, Risk: risk, Files: files}
}

func TestClassifyFinding(t *testing.T) {
	tests := []struct {
		name string
		f    pilotapi.Finding
		want string
	}{
		{"split_is_move_only", findingTouching("Split oversized file", "refactor", pilotapi.RiskMedium), classMoveOnly},
		{"decompose_is_move_only", findingTouching("Decompose handler", "refactor", pilotapi.RiskMedium), classMoveOnly},
		{"loc_threshold_is_move_only", pilotapi.Finding{Title: "Split N file(s) under the LOC threshold", Kind: "refactor"}, classMoveOnly},
		{"todo_is_cleanup", findingTouching("Resolve TODO markers", "hardening", pilotapi.RiskLow), classCleanup},
		{"unused_dep_is_cleanup", findingTouching("Drop unused dependency", "refactor", pilotapi.RiskLow), classCleanup},
		{"cycle_is_behavior", findingTouching("Break package import cycle", "refactor", pilotapi.RiskHigh), classBehavior},
		{"layer_fix_is_behavior", findingTouching("Fix architectural layer violation", "hardening", pilotapi.RiskHigh), classBehavior},
		{"coverage_is_behavior", findingTouching("Raise coverage for package", "test-gap", pilotapi.RiskMedium), classBehavior},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyFinding(tt.f); got != tt.want {
				t.Fatalf("classifyFinding(%q) = %q, want %q", tt.f.Title, got, tt.want)
			}
		})
	}
}

func TestBlastRadiusFor(t *testing.T) {
	g := planGraph()
	tests := []struct {
		name  string
		files []string
		want  int
	}{
		{"leaf_zero", []string{"internal/leaf/x.go"}, 0},
		{"top_zero", []string{"internal/top/x.go"}, 0},
		{"mid_one", []string{"internal/mid/x.go"}, 1},
		{"core_two", []string{"internal/core/x.go"}, 2},
		{"unmapped_zero", []string{"docs/readme.md"}, 0},
		{"empty_zero", nil, 0},
		{"union_of_two", []string{"internal/core/x.go", "internal/mid/y.go"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blastRadiusFor(tt.files, g); got != tt.want {
				t.Fatalf("blastRadiusFor(%v) = %d, want %d", tt.files, got, tt.want)
			}
		})
	}
	if got := blastRadiusFor([]string{"internal/core/x.go"}, nil); got != 0 {
		t.Fatalf("nil graph blast radius = %d, want 0", got)
	}
}

// TestProjectToEpic_OrderingRespectsDependency proves leaf/low-blast changes are
// ordered before the higher-blast packages they underpin, and that the
// dependency chain is encoded in DependsOn.
func TestProjectToEpic_OrderingRespectsDependency(t *testing.T) {
	g := planGraph()
	findings := []pilotapi.Finding{
		findingTouching("Split core", "refactor", pilotapi.RiskMedium, "internal/core/big.go"), // blast 2
		findingTouching("Split mid", "refactor", pilotapi.RiskMedium, "internal/mid/big.go"),   // blast 1
		findingTouching("Split leaf", "refactor", pilotapi.RiskMedium, "internal/leaf/big.go"), // blast 0
	}

	plan := ProjectToEpic(findings, g, nil)
	if len(plan.PRs) != 3 {
		t.Fatalf("want 3 PRs, got %d", len(plan.PRs))
	}

	// leaf (0) < mid (1) < core (2).
	wantTitles := []string{"Split leaf", "Split mid", "Split core"}
	for i, pr := range plan.PRs {
		if pr.Title != wantTitles[i] {
			t.Fatalf("PR[%d] = %q, want %q (blast=%d)", i, pr.Title, wantTitles[i], pr.BlastRadius)
		}
		if pr.Order != i+1 {
			t.Fatalf("PR[%d].Order = %d, want %d", i, pr.Order, i+1)
		}
	}

	// Each step up in blast radius depends on the prior PR.
	if len(plan.PRs[0].DependsOn) != 0 {
		t.Fatalf("leaf PR must be a root, got DependsOn=%v", plan.PRs[0].DependsOn)
	}
	if !reflect.DeepEqual(plan.PRs[1].DependsOn, []int{1}) {
		t.Fatalf("mid PR DependsOn = %v, want [1]", plan.PRs[1].DependsOn)
	}
	if !reflect.DeepEqual(plan.PRs[2].DependsOn, []int{2}) {
		t.Fatalf("core PR DependsOn = %v, want [2]", plan.PRs[2].DependsOn)
	}
}

// TestProjectToEpic_EqualBlastSiblingsAreIndependent proves equal-blast PRs are
// parallelisable (no fabricated dependency edge).
func TestProjectToEpic_EqualBlastSiblingsAreIndependent(t *testing.T) {
	g := planGraph()
	findings := []pilotapi.Finding{
		findingTouching("Split leaf", "refactor", pilotapi.RiskMedium, "internal/leaf/a.go"),
		findingTouching("Split top", "refactor", pilotapi.RiskMedium, "internal/top/b.go"),
	}
	plan := ProjectToEpic(findings, g, nil)
	for _, pr := range plan.PRs {
		if pr.BlastRadius != 0 {
			t.Fatalf("%q expected blast 0, got %d", pr.Title, pr.BlastRadius)
		}
		if len(pr.DependsOn) != 0 {
			t.Fatalf("equal-blast %q must be independent, got DependsOn=%v", pr.Title, pr.DependsOn)
		}
	}
}

func TestPilotSafety(t *testing.T) {
	tests := []struct {
		name      string
		class     string
		blast     int
		risk      pilotapi.RiskLevel
		wantSafe  bool
		reasonHas string
	}{
		{"move_only_low_blast_safe", classMoveOnly, 1, pilotapi.RiskMedium, true, ""},
		{"cleanup_safe", classCleanup, 0, pilotapi.RiskLow, true, ""},
		{"high_blast_manual", classMoveOnly, highBlastRadius, pilotapi.RiskLow, false, "blast radius"},
		{"behavior_high_risk_manual", classBehavior, 1, pilotapi.RiskHigh, false, "behaviour change"},
		{"behavior_release_blocker_manual", classBehavior, 1, pilotapi.RiskReleaseBlocker, false, "behaviour change"},
		{"behavior_medium_risk_safe", classBehavior, 1, pilotapi.RiskMedium, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := pilotapi.Finding{Risk: tt.risk}
			safe, reason := pilotSafety(tt.class, tt.blast, f)
			if safe != tt.wantSafe {
				t.Fatalf("pilotSafety safe=%v, want %v (reason %q)", safe, tt.wantSafe, reason)
			}
			if !tt.wantSafe && tt.reasonHas != "" && !containsSub(reason, tt.reasonHas) {
				t.Fatalf("reason %q missing %q", reason, tt.reasonHas)
			}
			if !tt.wantSafe && reason == "" {
				t.Fatalf("manual verdict must carry a reason")
			}
			if tt.wantSafe && reason != "" {
				t.Fatalf("pilot-safe verdict must carry no reason, got %q", reason)
			}
		})
	}
}

// TestProjectToEpic_ManualFlagAndCount proves the high-blast / risky behaviour
// units are flagged manual and counted.
func TestProjectToEpic_ManualFlagAndCount(t *testing.T) {
	g := planGraph()
	findings := []pilotapi.Finding{
		findingTouching("Split leaf", "refactor", pilotapi.RiskMedium, "internal/leaf/a.go"),               // safe
		findingTouching("Break import cycle in core", "refactor", pilotapi.RiskHigh, "internal/core/c.go"), // behavior + blast 2, high risk -> manual
	}
	plan := ProjectToEpic(findings, g, nil)
	if plan.Manual != 1 {
		t.Fatalf("Manual = %d, want 1", plan.Manual)
	}
	for _, pr := range plan.PRs {
		if pr.Class == classBehavior && pr.PilotSafe {
			t.Fatalf("high-risk behaviour PR %q must be manual", pr.Title)
		}
		if !pr.PilotSafe && pr.ManualReason == "" {
			t.Fatalf("manual PR %q must carry a reason", pr.Title)
		}
	}
}

// TestProjectToEpic_CapAtMax proves a plan past maxPlanSubtasks is truncated to
// the leaf-first highest-priority PRs.
func TestProjectToEpic_CapAtMax(t *testing.T) {
	var findings []pilotapi.Finding
	for i := 0; i < maxPlanSubtasks+8; i++ {
		findings = append(findings, findingTouching(fmt.Sprintf("Split file %02d", i), "refactor", pilotapi.RiskMedium, "pkg/f.go"))
	}
	plan := ProjectToEpic(findings, nil, nil)
	if len(plan.PRs) != maxPlanSubtasks {
		t.Fatalf("plan capped to %d, got %d", maxPlanSubtasks, len(plan.PRs))
	}
	if len(plan.Epic.Subtasks) != maxPlanSubtasks {
		t.Fatalf("epic subtasks = %d, want %d", len(plan.Epic.Subtasks), maxPlanSubtasks)
	}
	// Orders are contiguous 1..max.
	for i, pr := range plan.PRs {
		if pr.Order != i+1 {
			t.Fatalf("PR[%d].Order = %d, want %d", i, pr.Order, i+1)
		}
	}
}

func TestProjectToEpic_BelowMinNotPadded(t *testing.T) {
	findings := []pilotapi.Finding{
		findingTouching("Split a", "refactor", pilotapi.RiskMedium, "pkg/a.go"),
		findingTouching("Split b", "refactor", pilotapi.RiskMedium, "pkg/b.go"),
	}
	plan := ProjectToEpic(findings, nil, nil)
	if len(plan.PRs) != 2 {
		t.Fatalf("below-min plan must keep its real %d PRs, got %d", 2, len(plan.PRs))
	}
}

// TestProjectToEpic_Deterministic proves identical inputs produce byte-identical
// ordered plans and epic projections across repeated runs.
func TestProjectToEpic_Deterministic(t *testing.T) {
	g := planGraph()
	findings := []pilotapi.Finding{
		findingTouching("Split core", "refactor", pilotapi.RiskMedium, "internal/core/a.go"),
		findingTouching("Split mid", "refactor", pilotapi.RiskHigh, "internal/mid/b.go"),
		findingTouching("Resolve TODO", "hardening", pilotapi.RiskLow, "internal/leaf/c.go"),
		findingTouching("Split leaf", "refactor", pilotapi.RiskMedium, "internal/leaf/d.go"),
	}
	first := ProjectToEpic(findings, g, nil)
	for i := 0; i < 5; i++ {
		again := ProjectToEpic(findings, g, nil)
		if !reflect.DeepEqual(first.PRs, again.PRs) {
			t.Fatalf("non-deterministic PRs on run %d", i)
		}
		if !reflect.DeepEqual(first.Epic, again.Epic) {
			t.Fatalf("non-deterministic Epic on run %d", i)
		}
	}
}

// TestProjectToEpic_EpicProjection proves the EpicPlan mirrors the PR sequence:
// same count, same Order/DependsOn, a parent task, and a self-contained body.
func TestProjectToEpic_EpicProjection(t *testing.T) {
	g := planGraph()
	owners := map[string]string{"internal/core/a.go": "Ada"}
	findings := []pilotapi.Finding{
		findingTouching("Split leaf", "refactor", pilotapi.RiskMedium, "internal/leaf/x.go"),
		findingTouching("Split core", "refactor", pilotapi.RiskMedium, "internal/core/a.go"),
	}
	plan := ProjectToEpic(findings, g, owners)

	if plan.Epic == nil || plan.Epic.ParentTask == nil || plan.Epic.ParentTask.Title != epicParentTitle {
		t.Fatalf("epic parent task missing/wrong: %+v", plan.Epic)
	}
	if len(plan.Epic.Subtasks) != len(plan.PRs) {
		t.Fatalf("epic subtasks %d != PRs %d", len(plan.Epic.Subtasks), len(plan.PRs))
	}
	for i, sub := range plan.Epic.Subtasks {
		pr := plan.PRs[i]
		if sub.Title != pr.Title || sub.Order != pr.Order {
			t.Fatalf("subtask[%d] mismatch: %+v vs PR %+v", i, sub, pr)
		}
		if !reflect.DeepEqual(sub.DependsOn, pr.DependsOn) {
			t.Fatalf("subtask[%d] DependsOn %v != PR %v", i, sub.DependsOn, pr.DependsOn)
		}
		if sub.Description == "" {
			t.Fatalf("subtask[%d] body must be self-contained", i)
		}
	}
	// The core PR carries the owner annotation in its body.
	var coreBody string
	for _, sub := range plan.Epic.Subtasks {
		if sub.Title == "Split core" {
			coreBody = sub.Description
		}
	}
	if !containsSub(coreBody, "Ada") {
		t.Fatalf("core subtask body missing owner annotation: %q", coreBody)
	}
}

func TestProjectToEpic_EmptyFindings(t *testing.T) {
	plan := ProjectToEpic(nil, planGraph(), nil)
	if len(plan.PRs) != 0 {
		t.Fatalf("empty findings => %d PRs, want 0", len(plan.PRs))
	}
	if plan.Epic == nil || len(plan.Epic.Subtasks) != 0 {
		t.Fatalf("empty findings => epic must be non-nil with no subtasks, got %+v", plan.Epic)
	}
	if plan.Manual != 0 {
		t.Fatalf("empty findings => Manual %d, want 0", plan.Manual)
	}
}

func TestOwnerForFiles(t *testing.T) {
	owners := map[string]string{"a.go": "Ada", "b.go": "Ada", "c.go": "Bob"}
	if got := ownerForFiles([]string{"a.go", "b.go", "c.go"}, owners); got != "Ada" {
		t.Fatalf("ownerForFiles = %q, want Ada (majority)", got)
	}
	if got := ownerForFiles([]string{"unknown.go"}, owners); got != "" {
		t.Fatalf("ownerForFiles(unknown) = %q, want empty", got)
	}
	if got := ownerForFiles([]string{"a.go"}, nil); got != "" {
		t.Fatalf("ownerForFiles(nil owners) = %q, want empty", got)
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

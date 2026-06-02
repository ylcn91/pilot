package github

import (
	"testing"
	"time"
)

func TestGroupIssues_MixedFlatTasks(t *testing.T) {
	now := time.Now()

	// Epic parent.
	epicParent := &Issue{
		Number:    300,
		Title:     "Epic: Payment integration",
		State:     "open",
		CreatedAt: now.Add(-10 * time.Hour),
	}
	// Epic children — 1 done, 1 in progress.
	epicChild1 := &Issue{
		Number:    301,
		Title:     "Stripe setup",
		Body:      "Parent: GH-300\n\nSetup Stripe SDK",
		State:     "closed",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-9 * time.Hour),
	}
	epicChild2 := &Issue{
		Number:    302,
		Title:     "Payment webhook",
		Body:      "Parent: GH-300\n\nHandle webhooks",
		State:     "open",
		Labels:    []Label{{Name: LabelInProgress}},
		CreatedAt: now.Add(-8 * time.Hour),
	}

	// Standalone open issue.
	standalone1 := &Issue{
		Number:    400,
		Title:     "Fix typo in README",
		State:     "open",
		CreatedAt: now.Add(-5 * time.Hour),
	}

	// Standalone closed issue.
	standalone2 := &Issue{
		Number:    500,
		Title:     "Update CI config",
		State:     "closed",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-7 * time.Hour),
	}

	// Completed epic.
	epicParent2 := &Issue{
		Number:    600,
		Title:     "Epic: Auth system",
		State:     "closed",
		CreatedAt: now.Add(-20 * time.Hour),
	}
	epicChild3 := &Issue{
		Number:    601,
		Title:     "Login endpoint",
		Body:      "Parent: GH-600\n\nImplement login",
		State:     "closed",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-19 * time.Hour),
	}

	issues := []*Issue{
		epicParent, epicChild1, epicChild2,
		standalone1, standalone2,
		epicParent2, epicChild3,
	}

	result := GroupIssues(issues)

	// Children absorbed: 7 issues → 4 grouped (2 epics + 2 standalone).
	if len(result) != 4 {
		t.Fatalf("expected 4 grouped issues, got %d", len(result))
	}

	// Verify ordering: active epics → active standalone → completed epics → completed standalone.
	// Active epic (300) should come first.
	if result[0].Issue.Number != 300 {
		t.Errorf("expected first item to be active epic 300, got %d", result[0].Issue.Number)
	}
	if !result[0].IsActive || !result[0].IsEpic {
		t.Errorf("item 0: expected active epic, got IsActive=%v IsEpic=%v", result[0].IsActive, result[0].IsEpic)
	}
	if result[0].TotalSubs != 2 || result[0].DoneSubs != 1 {
		t.Errorf("epic 300: expected TotalSubs=2 DoneSubs=1, got %d/%d", result[0].TotalSubs, result[0].DoneSubs)
	}

	// Active standalone (400).
	if result[1].Issue.Number != 400 {
		t.Errorf("expected second item to be active standalone 400, got %d", result[1].Issue.Number)
	}
	if !result[1].IsActive || result[1].IsEpic {
		t.Errorf("item 1: expected active standalone, got IsActive=%v IsEpic=%v", result[1].IsActive, result[1].IsEpic)
	}

	// Completed epic (600).
	if result[2].Issue.Number != 600 {
		t.Errorf("expected third item to be completed epic 600, got %d", result[2].Issue.Number)
	}
	if result[2].IsActive || !result[2].IsEpic {
		t.Errorf("item 2: expected completed epic, got IsActive=%v IsEpic=%v", result[2].IsActive, result[2].IsEpic)
	}
	if result[2].DoneSubs != 1 || result[2].TotalSubs != 1 {
		t.Errorf("epic 600: expected TotalSubs=1 DoneSubs=1, got %d/%d", result[2].TotalSubs, result[2].DoneSubs)
	}

	// Completed standalone (500).
	if result[3].Issue.Number != 500 {
		t.Errorf("expected fourth item to be completed standalone 500, got %d", result[3].Issue.Number)
	}
	if result[3].IsActive || result[3].IsEpic {
		t.Errorf("item 3: expected completed standalone, got IsActive=%v IsEpic=%v", result[3].IsActive, result[3].IsEpic)
	}

	// Verify no children appear as top-level.
	childNums := map[int]bool{301: true, 302: true, 601: true}
	for _, g := range result {
		if childNums[g.Issue.Number] {
			t.Errorf("child issue %d should not appear as top-level", g.Issue.Number)
		}
	}
}

func TestGroupIssues_OrphanChild(t *testing.T) {
	// Child references parent that isn't in the list — treated as standalone.
	now := time.Now()
	orphan := &Issue{
		Number:    50,
		Title:     "Orphan subtask",
		Body:      "Parent: GH-999\n\nParent not in list",
		State:     "open",
		CreatedAt: now,
	}

	result := GroupIssues([]*Issue{orphan})

	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}

	// Orphan references a parent not in the list — treated as standalone.
	g := result[0]
	if g.Issue.Number != 50 {
		t.Errorf("expected orphan issue 50, got %d", g.Issue.Number)
	}
	if g.IsEpic {
		t.Error("orphan with missing parent should be standalone, not epic")
	}
	if !g.IsActive {
		t.Error("open orphan should be active")
	}
}

func TestGroupIssues_DoneByLabelOnly(t *testing.T) {
	// Issue is "open" state but has pilot-done label — should count as done.
	now := time.Now()
	parent := &Issue{
		Number:    700,
		Title:     "Epic",
		State:     "open",
		CreatedAt: now.Add(-2 * time.Hour),
	}
	child := &Issue{
		Number:    701,
		Title:     "Sub",
		Body:      "Parent: GH-700\n\nWork",
		State:     "open",
		Labels:    []Label{{Name: LabelDone}},
		CreatedAt: now.Add(-1 * time.Hour),
	}

	result := GroupIssues([]*Issue{parent, child})
	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}

	epic := result[0]
	if epic.DoneSubs != 1 {
		t.Errorf("expected DoneSubs=1 (done by label), got %d", epic.DoneSubs)
	}
	if epic.IsActive {
		t.Error("expected epic to be completed (only child is done by label)")
	}
}

func TestGroupIssues_StandaloneClosedInactive(t *testing.T) {
	now := time.Now()
	issue := &Issue{
		Number:    800,
		Title:     "Closed standalone",
		State:     "closed",
		CreatedAt: now,
	}

	result := GroupIssues([]*Issue{issue})
	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}
	if result[0].IsActive {
		t.Error("closed standalone issue should not be active")
	}
}

func TestGroupIssues_HashStyleParent(t *testing.T) {
	now := time.Now()
	parent := &Issue{
		Number:    900,
		Title:     "Epic with hash children",
		State:     "open",
		CreatedAt: now.Add(-2 * time.Hour),
	}
	child := &Issue{
		Number:    901,
		Title:     "Hash child",
		Body:      "Parent: #900\n\nWork",
		State:     "open",
		CreatedAt: now.Add(-1 * time.Hour),
	}

	result := GroupIssues([]*Issue{parent, child})
	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}
	if !result[0].IsEpic {
		t.Error("expected issue with hash-style child to be detected as epic")
	}
	if result[0].TotalSubs != 1 {
		t.Errorf("expected TotalSubs=1, got %d", result[0].TotalSubs)
	}
}

func TestGroupIssues_SubIssueSortOrder(t *testing.T) {
	now := time.Now()
	parent := &Issue{
		Number:    1000,
		Title:     "Epic",
		State:     "open",
		CreatedAt: now.Add(-10 * time.Hour),
	}
	// Children added in reverse order.
	child3 := &Issue{
		Number:    1003,
		Title:     "Third",
		Body:      "Parent: GH-1000\n\n3rd",
		State:     "open",
		CreatedAt: now.Add(-1 * time.Hour),
	}
	child1 := &Issue{
		Number:    1001,
		Title:     "First",
		Body:      "Parent: GH-1000\n\n1st",
		State:     "open",
		CreatedAt: now.Add(-3 * time.Hour),
	}
	child2 := &Issue{
		Number:    1002,
		Title:     "Second",
		Body:      "Parent: GH-1000\n\n2nd",
		State:     "open",
		CreatedAt: now.Add(-2 * time.Hour),
	}

	// Input in arbitrary order.
	issues := []*Issue{child3, parent, child1, child2}
	result := GroupIssues(issues)

	if len(result) != 1 {
		t.Fatalf("expected 1 grouped issue, got %d", len(result))
	}

	subs := result[0].SubIssues
	if len(subs) != 3 {
		t.Fatalf("expected 3 sub-issues, got %d", len(subs))
	}

	expectedOrder := []int{1001, 1002, 1003}
	for i, expected := range expectedOrder {
		if subs[i].Number != expected {
			t.Errorf("sub-issue at index %d: expected #%d, got #%d", i, expected, subs[i].Number)
		}
	}
}

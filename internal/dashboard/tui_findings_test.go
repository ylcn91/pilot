package dashboard

import (
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// sampleFindings returns a representative set of findings covering every
// canonical risk level plus varied file counts.
func sampleFindings() []pilotapi.Finding {
	return []pilotapi.Finding{
		{
			Title: "Unbounded goroutine spawn in poller",
			Kind:  "concurrency",
			Risk:  pilotapi.RiskReleaseBlocker,
			Files: []string{"internal/gateway/poller.go", "internal/gateway/server.go"},
		},
		{
			Title: "Missing context cancellation on HTTP client",
			Kind:  "reliability",
			Risk:  pilotapi.RiskHigh,
			Files: []string{"internal/adapters/github/client.go"},
		},
		{
			Title: "Config default drifts from documented value",
			Kind:  "config",
			Risk:  pilotapi.RiskMedium,
			Files: []string{"internal/config/config.go"},
		},
		{
			Title: "Redundant slice copy in render loop",
			Kind:  "perf",
			Risk:  pilotapi.RiskLow,
			Files: []string{},
		},
	}
}

// --- UpdateFindings command produces the expected message ---

func TestUpdateFindings_ProducesMsg(t *testing.T) {
	findings := sampleFindings()
	cmd := UpdateFindings(findings)
	if cmd == nil {
		t.Fatal("UpdateFindings returned nil cmd")
	}

	msg := cmd()
	got, ok := msg.(updateFindingsMsg)
	if !ok {
		t.Fatalf("msg type = %T, want updateFindingsMsg", msg)
	}
	if len(got) != len(findings) {
		t.Fatalf("msg len = %d, want %d", len(got), len(findings))
	}
	for i := range findings {
		if got[i].Title != findings[i].Title {
			t.Errorf("finding[%d].Title = %q, want %q", i, got[i].Title, findings[i].Title)
		}
		if got[i].Risk != findings[i].Risk {
			t.Errorf("finding[%d].Risk = %q, want %q", i, got[i].Risk, findings[i].Risk)
		}
	}
}

func TestUpdateFindings_EmptySlice(t *testing.T) {
	cmd := UpdateFindings([]pilotapi.Finding{})
	if cmd == nil {
		t.Fatal("UpdateFindings returned nil cmd for empty slice")
	}
	msg, ok := cmd().(updateFindingsMsg)
	if !ok {
		t.Fatalf("msg type = %T, want updateFindingsMsg", cmd())
	}
	if len(msg) != 0 {
		t.Errorf("empty slice produced %d findings", len(msg))
	}
}

func TestUpdateFindings_NilSlice(t *testing.T) {
	cmd := UpdateFindings(nil)
	if cmd == nil {
		t.Fatal("UpdateFindings returned nil cmd for nil slice")
	}
	msg, ok := cmd().(updateFindingsMsg)
	if !ok {
		t.Fatalf("msg type = %T, want updateFindingsMsg", cmd())
	}
	if len(msg) != 0 {
		t.Errorf("nil slice produced %d findings", len(msg))
	}
}

// --- Update() stores findings on the model ---

func TestUpdate_StoresFindings(t *testing.T) {
	m := NewModel("test")
	findings := sampleFindings()

	updated, _ := m.Update(updateFindingsMsg(findings))
	mm := updated.(Model)

	if len(mm.findings) != len(findings) {
		t.Fatalf("model.findings len = %d, want %d", len(mm.findings), len(findings))
	}
	for i := range findings {
		if mm.findings[i].Title != findings[i].Title {
			t.Errorf("stored finding[%d].Title = %q, want %q",
				i, mm.findings[i].Title, findings[i].Title)
		}
	}
}

func TestUpdate_FindingsCountChange_ClearsScreen(t *testing.T) {
	m := NewModel("test")
	// Going from 0 to N findings changes panel height → must request repaint.
	updated, cmd := m.Update(updateFindingsMsg(sampleFindings()))
	if cmd == nil {
		t.Fatal("expected ClearScreen cmd on findings count change, got nil")
	}
	// Re-applying the same count must NOT request a repaint.
	mm := updated.(Model)
	_, cmd2 := mm.Update(updateFindingsMsg(sampleFindings()))
	if cmd2 != nil {
		t.Errorf("expected nil cmd when finding count unchanged, got %T", cmd2())
	}
}

func TestUpdate_FindingsReplaced(t *testing.T) {
	m := NewModel("test")
	updated, _ := m.Update(updateFindingsMsg(sampleFindings()))
	mm := updated.(Model)

	// Replace with a smaller, different set.
	replacement := []pilotapi.Finding{
		{Title: "Only one left", Risk: pilotapi.RiskLow, Files: []string{"a.go"}},
	}
	updated2, _ := mm.Update(updateFindingsMsg(replacement))
	mm2 := updated2.(Model)

	if len(mm2.findings) != 1 {
		t.Fatalf("findings len = %d, want 1 after replacement", len(mm2.findings))
	}
	if mm2.findings[0].Title != "Only one left" {
		t.Errorf("findings[0].Title = %q, want %q", mm2.findings[0].Title, "Only one left")
	}
}

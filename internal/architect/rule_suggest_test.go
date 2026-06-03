package architect

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// pitfallSig builds a known_pitfall fixture Signal carrying memory content.
func pitfallSig(content string) Signal {
	return Signal{Kind: KnownPitfallKind, Detail: content, Risk: pilotapi.RiskMedium}
}

// decisionSig builds a known_decision fixture Signal carrying memory content.
func decisionSig(content string) Signal {
	return Signal{Kind: KnownDecisionKind, Detail: content, Risk: pilotapi.RiskMedium}
}

// violationSig builds a layer_violation fixture Signal whose Detail mirrors the
// live rendering produced by the deps collector.
func violationSig(from, to string) Signal {
	return Signal{
		Kind:   kindLayerViolation,
		File:   from,
		Detail: "forbidden import " + from + " -> " + to + " (rule x: y)",
		Risk:   pilotapi.RiskHigh,
	}
}

func TestRuleSuggester_DisabledYieldsNothing(t *testing.T) {
	s := NewRuleSuggester(false, defaultLayerRules)
	got := s.Suggest([]Signal{
		pitfallSig("internal/gateway must not import internal/executor"),
		pitfallSig("internal/gateway must not import internal/executor"),
	})
	if got != nil {
		t.Fatalf("disabled suggester must return nil; got %+v", got)
	}
}

func TestRuleSuggester_MinesRecurringEdgeFromMemory(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	got := s.Suggest([]Signal{
		pitfallSig("internal/gateway must not import internal/executor"),
		decisionSig("internal/gateway depends on internal/executor and that inverts layering"),
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(got), got)
	}
	sig := got[0]
	if sig.Kind != KindRuleSuggestion {
		t.Errorf("Kind = %q, want %q", sig.Kind, KindRuleSuggestion)
	}
	if sig.File != "internal/gateway" {
		t.Errorf("File = %q, want internal/gateway", sig.File)
	}
	if sig.Risk != pilotapi.RiskLow {
		t.Errorf("Risk = %q, want low (advisory)", sig.Risk)
	}
	if !strings.Contains(sig.Detail, "DRAFT") {
		t.Errorf("draft must be marked DRAFT; got: %s", sig.Detail)
	}
	if !strings.Contains(sig.Detail, `From: "internal/gateway"`) ||
		!strings.Contains(sig.Detail, `To: "internal/executor"`) {
		t.Errorf("draft must render the From->To edge; got: %s", sig.Detail)
	}
	if !strings.Contains(sig.Detail, "gateway-must-not-import-executor") {
		t.Errorf("draft must carry a derived rule name; got: %s", sig.Detail)
	}
	// Triggers from both the pitfall and the decision are linked.
	if !strings.Contains(sig.Detail, KnownPitfallKind) || !strings.Contains(sig.Detail, KnownDecisionKind) {
		t.Errorf("draft must link both triggering memories; got: %s", sig.Detail)
	}
}

func TestRuleSuggester_AggregatesRepeatedViolations(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	got := s.Suggest([]Signal{
		violationSig("internal/adapters/foo", "internal/dashboard"),
		violationSig("internal/adapters/foo", "internal/dashboard"),
	})
	if len(got) != 1 {
		t.Fatalf("two violations on one unseen edge should yield 1 suggestion, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Detail, `From: "internal/adapters/foo"`) ||
		!strings.Contains(got[0].Detail, `To: "internal/dashboard"`) {
		t.Errorf("aggregated edge mismatch; got: %s", got[0].Detail)
	}
}

func TestRuleSuggester_SingleTriggerBelowThreshold(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	got := s.Suggest([]Signal{
		pitfallSig("internal/gateway must not import internal/executor"),
	})
	if len(got) != 0 {
		t.Fatalf("a single trigger is below threshold; want 0, got %d: %+v", len(got), got)
	}
}

func TestRuleSuggester_DedupAgainstDefaultLayerRules(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	// executor->config is already covered by defaultLayerRules
	// ("executor-must-not-import-config"), so even repeated signals must not
	// produce a new suggestion.
	got := s.Suggest([]Signal{
		pitfallSig("internal/executor must not import internal/config"),
		violationSig("internal/executor/sub", "internal/config"),
	})
	if len(got) != 0 {
		t.Fatalf("edge already in defaultLayerRules must not be suggested; got %d: %+v", len(got), got)
	}
}

func TestRuleSuggester_IgnoresNonPackageProse(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	// "Auth changes break tests" carries the verb "import" nowhere and the
	// tokens are bare words, so no edge is extracted.
	got := s.Suggest([]Signal{
		pitfallSig("Auth changes break tests sometimes"),
		pitfallSig("Remember to run go test before pushing"),
	})
	if len(got) != 0 {
		t.Fatalf("free prose must not produce edges; got %d: %+v", len(got), got)
	}
}

func TestRuleSuggester_DuplicateTriggersDoNotInflateWeight(t *testing.T) {
	s := NewRuleSuggester(true, defaultLayerRules)
	// Two identical pitfalls (same mined origin) plus one violation on the same
	// edge: only two DISTINCT triggers, so Weight must be 2 — not 3 — and the
	// reason text must report 2 recorded signals.
	got := s.Suggest([]Signal{
		pitfallSig("internal/gateway must not import internal/executor"),
		pitfallSig("internal/gateway must not import internal/executor"),
		violationSig("internal/gateway", "internal/executor"),
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(got), got)
	}
	if got[0].Weight != 2 {
		t.Errorf("Weight = %v, want 2 (distinct triggers, duplicates collapsed)", got[0].Weight)
	}
	if !strings.Contains(got[0].Detail, "suggested from 2 recorded signal(s)") {
		t.Errorf("reason must report 2 distinct triggers; got: %s", got[0].Detail)
	}
}

func TestRuleSuggester_Deterministic(t *testing.T) {
	in := []Signal{
		pitfallSig("internal/gateway must not import internal/executor"),
		violationSig("internal/gateway", "internal/executor"),
		decisionSig("internal/alerts depends on internal/dashboard"),
		violationSig("internal/alerts", "internal/dashboard"),
	}
	s := NewRuleSuggester(true, defaultLayerRules)
	first := s.Suggest(in)
	second := s.Suggest(in)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("suggester must be deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("expected 2 distinct edges, got %d: %+v", len(first), first)
	}
	// Sorted by From: alerts < gateway.
	if first[0].File != "internal/alerts" || first[1].File != "internal/gateway" {
		t.Errorf("suggestions must be sorted by From; got %q then %q", first[0].File, first[1].File)
	}
}

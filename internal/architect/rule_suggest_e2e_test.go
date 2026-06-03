package architect

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/memory"
)

// suggesterKnowledgeSource returns a fake PitfallSource whose pitfall and
// decision memories both encode the same forbidden edge, so the rule-suggester
// clears its two-trigger threshold on an edge defaultLayerRules does not cover.
func suggesterKnowledgeSource() *mockPitfallSource {
	return &mockPitfallSource{byType: map[memory.MemoryType][]*memory.Memory{
		memory.MemoryTypePitfall: {
			{Content: "internal/gateway must not import internal/executor", Context: "internal/gateway", Confidence: 0.9},
		},
		memory.MemoryTypeDecision: {
			{Content: "internal/gateway depends on internal/executor and that inverts layering", Context: "internal/gateway", Confidence: 0.8},
		},
	}}
}

// TestBuildLensScanner_SuggestRulesSurfacesSuggestion proves the gap3 suggester
// is reachable end-to-end through the Scanner the CLI builds: with
// SuggestRules=true and a knowledge source, a scan over the core lens runs the
// pitfall/decision collectors and the spliced post-pass, so a rule_suggestion
// Signal surfaces from Scan without any change to the runner.
func TestBuildLensScanner_SuggestRulesSurfacesSuggestion(t *testing.T) {
	scanner, err := BuildLensScanner("core", t.TempDir(), ScanOptions{
		SuggestRules: true,
		Memory: MemoryOptions{
			KnowledgeSource: suggesterKnowledgeSource(),
			ProjectID:       "proj",
		},
	})
	if err != nil {
		t.Fatalf("BuildLensScanner: %v", err)
	}

	signals, err := scanner.Scan(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var suggestion *Signal
	for i := range signals {
		if signals[i].Kind == KindRuleSuggestion {
			suggestion = &signals[i]
			break
		}
	}
	if suggestion == nil {
		t.Fatalf("expected a %s signal to surface; got kinds %v", KindRuleSuggestion, signalKinds(signals))
	}
	if suggestion.File != "internal/gateway" {
		t.Errorf("suggestion File = %q, want internal/gateway", suggestion.File)
	}
	if !strings.Contains(suggestion.Detail, "DRAFT") {
		t.Errorf("suggestion must be marked DRAFT; got: %s", suggestion.Detail)
	}
	if !strings.Contains(suggestion.Detail, `To: "internal/executor"`) {
		t.Errorf("suggestion must render the mined edge; got: %s", suggestion.Detail)
	}
}

// TestBuildLensScanner_SuggestRulesOffYieldsNoSuggestion proves the path stays
// inert by default: with SuggestRules=false the pitfall/decision collectors are
// not wired and the post-pass is nil, so the same knowledge source produces no
// rule_suggestion Signal.
func TestBuildLensScanner_SuggestRulesOffYieldsNoSuggestion(t *testing.T) {
	scanner, err := BuildLensScanner("core", t.TempDir(), ScanOptions{
		SuggestRules: false,
		Memory: MemoryOptions{
			KnowledgeSource: suggesterKnowledgeSource(),
			ProjectID:       "proj",
		},
	})
	if err != nil {
		t.Fatalf("BuildLensScanner: %v", err)
	}

	signals, err := scanner.Scan(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, s := range signals {
		if s.Kind == KindRuleSuggestion {
			t.Fatalf("SuggestRules=false must not surface a %s signal; got %+v", KindRuleSuggestion, s)
		}
	}
}

// TestRunConfig_SuggestRulesSurfacesFinding proves the suggestion flows all the
// way through the offline PROPOSE path into a Finding: a full RunConfig.Run with
// the deterministic analyzer turns the rule_suggestion Signal into a guardrail
// review Finding, with no GitHub creator (dry-run, nothing filed).
func TestRunConfig_SuggestRulesSurfacesFinding(t *testing.T) {
	scanner, err := BuildLensScanner("core", t.TempDir(), ScanOptions{
		SuggestRules: true,
		Memory: MemoryOptions{
			KnowledgeSource: suggesterKnowledgeSource(),
			ProjectID:       "proj",
		},
	})
	if err != nil {
		t.Fatalf("BuildLensScanner: %v", err)
	}

	analyzer := NewAnalyzer(nil, executor.BackendConfig{}, t.TempDir(), WithOffline(true))

	res, err := Run(context.Background(), RunConfig{
		Scanner:     scanner,
		Analyzer:    analyzer,
		ProjectPath: t.TempDir(),
	}, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, f := range res.Findings {
		if strings.Contains(f.Title, "candidate guardrail rule") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a guardrail-rule Finding from the suggestion; got findings %+v", res.Findings)
	}
}

// signalKinds extracts the kinds of a signal slice for failure diagnostics.
func signalKinds(signals []Signal) []string {
	kinds := make([]string, len(signals))
	for i, s := range signals {
		kinds[i] = s.Kind
	}
	return kinds
}

package architect

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// TestBuildRFCDoc_ProseOverridesOffline proves backend-supplied prose replaces
// the deterministic template for the sections it fills, while the tiny-PR
// sequence stays deterministically rendered from the plan.
func TestBuildRFCDoc_ProseOverridesOffline(t *testing.T) {
	findings := SynthesizeFindings(rfcFixtureSignals())
	plan := ProjectToEpic(findings, planGraph(), nil)
	prose := RFCProse{
		Problem:      "BACKEND-PROBLEM-PROSE",
		Constraints:  "BACKEND-CONSTRAINTS-PROSE",
		Alternatives: "BACKEND-ALTERNATIVES-PROSE",
		Decision:     "BACKEND-DECISION-PROSE",
		Rollout:      "BACKEND-ROLLOUT-PROSE",
		Risk:         "BACKEND-RISK-PROSE",
	}
	doc := BuildRFCDoc("Backed", findings, plan, prose)

	for _, want := range []string{
		"BACKEND-PROBLEM-PROSE", "BACKEND-CONSTRAINTS-PROSE", "BACKEND-ALTERNATIVES-PROSE",
		"BACKEND-DECISION-PROSE", "BACKEND-ROLLOUT-PROSE", "BACKEND-RISK-PROSE",
	} {
		if !strings.Contains(doc.Body, want) {
			t.Fatalf("backend prose %q not used", want)
		}
	}
	// The deterministic offline boilerplate must be displaced by the prose.
	if strings.Contains(doc.Body, "A deterministic scan of this project surfaced the following") {
		t.Fatal("offline problem template should be replaced by backend prose")
	}
	// The sequence is still deterministic (carries PR annotations).
	if !strings.Contains(doc.Body, "blast=") {
		t.Fatal("tiny-PR sequence must still render even with backend prose")
	}
}

// TestBuildRFCDoc_PartialProseFallsBack proves a backend that fills only some
// sections still yields a complete document: blank sections fall back to the
// offline template.
func TestBuildRFCDoc_PartialProseFallsBack(t *testing.T) {
	findings := SynthesizeFindings(rfcFixtureSignals())
	plan := ProjectToEpic(findings, planGraph(), nil)
	doc := BuildRFCDoc("Partial", findings, plan, RFCProse{Decision: "ONLY-DECISION"})

	if !strings.Contains(doc.Body, "ONLY-DECISION") {
		t.Fatal("supplied decision prose must be used")
	}
	// Problem section was left blank => offline template fills it.
	if !strings.Contains(doc.Body, "A deterministic scan of this project surfaced") {
		t.Fatal("blank problem section must fall back to offline template")
	}
	// Every section heading still present.
	for _, sec := range []string{rfcSecProblem, rfcSecConstraints, rfcSecAlternatives, rfcSecRollout, rfcSecRisk} {
		if !strings.Contains(doc.Body, sec) {
			t.Fatalf("partial-prose doc missing section %q", sec)
		}
	}
}

// TestBuildRFCDoc_FrontmatterCounts proves the frontmatter reports the plan's PR
// and manual-review counts accurately.
func TestBuildRFCDoc_FrontmatterCounts(t *testing.T) {
	findings := SynthesizeFindings(rfcFixtureSignals())
	plan := ProjectToEpic(findings, planGraph(), nil)
	doc := BuildRFCDoc("Counts", findings, plan, RFCProse{})

	front := doc.Body[:strings.Index(doc.Body, "\n---\n\n")]
	if !strings.Contains(front, "pr_count:") {
		t.Fatalf("frontmatter missing pr_count:\n%s", front)
	}
	// pr_count must equal the actual number of rendered PR entries.
	wantPR := len(plan.PRs)
	if !strings.Contains(front, "pr_count: "+strconv.Itoa(wantPR)) {
		t.Fatalf("frontmatter pr_count != %d:\n%s", wantPR, front)
	}
	if !strings.Contains(front, "manual_review: "+strconv.Itoa(plan.Manual)) {
		t.Fatalf("frontmatter manual_review != %d:\n%s", plan.Manual, front)
	}
}

// TestBuildRFCDoc_BodyEndsWithNewline proves the rendered document is normalised
// to end with exactly one trailing newline.
func TestBuildRFCDoc_BodyEndsWithNewline(t *testing.T) {
	doc := BuildRFCDoc("Norm", nil, RefactorPlan{}, RFCProse{})
	if !strings.HasSuffix(doc.Body, "\n") || strings.HasSuffix(doc.Body, "\n\n") {
		t.Fatalf("body must end with exactly one newline; tail=%q", doc.Body)
	}
}

func TestHighestFindingRisk(t *testing.T) {
	tests := []struct {
		name     string
		findings []pilotapi.Finding
		want     pilotapi.RiskLevel
	}{
		{"empty_defaults_medium", nil, pilotapi.RiskMedium},
		{"single_low", []pilotapi.Finding{{Title: "a", Risk: pilotapi.RiskLow}}, pilotapi.RiskLow},
		{"max_wins", []pilotapi.Finding{
			{Title: "a", Risk: pilotapi.RiskLow},
			{Title: "b", Risk: pilotapi.RiskReleaseBlocker},
			{Title: "c", Risk: pilotapi.RiskHigh},
		}, pilotapi.RiskReleaseBlocker},
		{"blank_risk_normalised", []pilotapi.Finding{{Title: "a", Risk: pilotapi.RiskLevel("")}}, pilotapi.RiskMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := highestFindingRisk(tt.findings); got != tt.want {
				t.Fatalf("highestFindingRisk = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindingTitles_SortedDeduped(t *testing.T) {
	in := []pilotapi.Finding{
		{Title: "  B  "}, {Title: "A"}, {Title: "B"}, {Title: ""}, {Title: "A"},
	}
	got := findingTitles(in)
	want := []string{"A", "B"}
	if len(got) != len(want) {
		t.Fatalf("findingTitles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findingTitles[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

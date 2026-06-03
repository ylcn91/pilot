package autopilot

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestRenderGuardrailsComment_GroupsByRuleSorted(t *testing.T) {
	v := []architect.Violation{
		{Rule: "loc-400", File: "internal/z/big.go", Detail: "450 lines", Risk: pilotapi.RiskMedium},
		{Rule: "forbidden-import", File: "internal/a/bad.go", Detail: "executor imports config", Risk: pilotapi.RiskHigh},
		{Rule: "loc-400", File: "internal/a/big.go", Detail: "500 lines", Risk: pilotapi.RiskHigh},
	}
	out := renderGuardrailsComment(v, nil, nil, "report")

	// forbidden-import sorts before loc-400.
	fi := strings.Index(out, "### `forbidden-import`")
	loc := strings.Index(out, "### `loc-400`")
	if fi < 0 || loc < 0 {
		t.Fatalf("both rule headings must appear:\n%s", out)
	}
	if fi > loc {
		t.Errorf("rules must be sorted (forbidden-import before loc-400):\n%s", out)
	}

	// Within loc-400, internal/a/big.go (a) must precede internal/z/big.go (z).
	aBig := strings.Index(out, "internal/a/big.go")
	zBig := strings.Index(out, "internal/z/big.go")
	if aBig < 0 || zBig < 0 || aBig > zBig {
		t.Errorf("files within a rule must be sorted:\n%s", out)
	}
}

func TestRenderGuardrailsComment_MarkerAndMode(t *testing.T) {
	v := []architect.Violation{
		{Rule: "loc-400", File: "internal/x/big.go", Detail: "450 lines", Risk: pilotapi.RiskMedium},
	}

	report := renderGuardrailsComment(v, nil, nil, "report")
	if !strings.HasPrefix(report, guardrailsCommentMarker) {
		t.Errorf("comment must begin with the idempotency marker, got:\n%s", report)
	}
	if !strings.Contains(report, "report-only") {
		t.Errorf("report comment must mark itself report-only:\n%s", report)
	}
	if strings.Contains(report, "block mode") {
		t.Errorf("report comment must not claim block mode:\n%s", report)
	}

	block := renderGuardrailsComment(v, nil, nil, "block")
	if !strings.Contains(block, "block") {
		t.Errorf("block comment must mention block mode:\n%s", block)
	}
}

func TestRenderGuardrailsComment_SingularPlural(t *testing.T) {
	one := renderGuardrailsComment([]architect.Violation{
		{Rule: "loc-400", File: "a.go", Detail: "x", Risk: pilotapi.RiskLow},
	}, nil, nil, "report")
	if !strings.Contains(one, "**1** violation") || strings.Contains(one, "violations") {
		t.Errorf("single violation must read 'violation' (singular):\n%s", one)
	}

	two := renderGuardrailsComment([]architect.Violation{
		{Rule: "loc-400", File: "a.go", Detail: "x", Risk: pilotapi.RiskLow},
		{Rule: "loc-400", File: "b.go", Detail: "y", Risk: pilotapi.RiskLow},
	}, nil, nil, "report")
	if !strings.Contains(two, "**2** violations") {
		t.Errorf("two violations must read 'violations' (plural):\n%s", two)
	}
}

func TestRenderGuardrailsComment_RiskBadges(t *testing.T) {
	cases := []struct {
		risk pilotapi.RiskLevel
		want string
	}{
		{pilotapi.RiskHigh, "[high]"},
		{pilotapi.RiskMedium, "[medium]"},
		{pilotapi.RiskLow, "[low]"},
		{pilotapi.RiskLevel("weird"), "[info]"},
	}
	for _, tc := range cases {
		out := renderGuardrailsComment([]architect.Violation{
			{Rule: "loc-400", File: "a.go", Detail: "x", Risk: tc.risk},
		}, nil, nil, "report")
		if !strings.Contains(out, tc.want) {
			t.Errorf("risk %q must render badge %q, got:\n%s", tc.risk, tc.want, out)
		}
	}
}

func TestRenderGuardrailsComment_DeterministicForUnsortedInput(t *testing.T) {
	a := []architect.Violation{
		{Rule: "b-rule", File: "z.go", Detail: "d", Risk: pilotapi.RiskLow},
		{Rule: "a-rule", File: "y.go", Detail: "d", Risk: pilotapi.RiskLow},
	}
	b := []architect.Violation{
		{Rule: "a-rule", File: "y.go", Detail: "d", Risk: pilotapi.RiskLow},
		{Rule: "b-rule", File: "z.go", Detail: "d", Risk: pilotapi.RiskLow},
	}
	if renderGuardrailsComment(a, nil, nil, "report") != renderGuardrailsComment(b, nil, nil, "report") {
		t.Error("comment rendering must be order-independent (deterministic)")
	}
}

func TestRiskBadge(t *testing.T) {
	if riskBadge(pilotapi.RiskHigh) != "[high]" {
		t.Error("high badge mismatch")
	}
	if riskBadge(pilotapi.RiskLevel("")) != "[info]" {
		t.Error("unknown risk must fall back to [info]")
	}
}

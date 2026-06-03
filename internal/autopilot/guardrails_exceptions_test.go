package autopilot

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestParseAllowedRules_Basic(t *testing.T) {
	got := parseAllowedRules("pilot-guardrail-allow: loc-400")
	if !reflect.DeepEqual(keys(got), []string{"loc-400"}) {
		t.Errorf("got %v", keys(got))
	}
}

func TestParseAllowedRules_Empty(t *testing.T) {
	if parseAllowedRules("") != nil {
		t.Error("empty text must yield nil")
	}
	if parseAllowedRules("nothing relevant here") != nil {
		t.Error("text without a directive must yield nil")
	}
}

func TestParseAllowedRules_DirectiveWithNoName(t *testing.T) {
	if got := parseAllowedRules("pilot-guardrail-allow:"); got != nil {
		t.Errorf("a bare directive with no rule must yield nil, got %v", keys(got))
	}
	if got := parseAllowedRules("pilot-guardrail-allow:    "); got != nil {
		t.Errorf("a directive with only whitespace must yield nil, got %v", keys(got))
	}
}

func TestParseAllowedRules_CaseInsensitiveDirective(t *testing.T) {
	got := parseAllowedRules("PILOT-GUARDRAIL-ALLOW: forbidden-import")
	if !reflect.DeepEqual(keys(got), []string{"forbidden-import"}) {
		t.Errorf("directive match must be case-insensitive, got %v", keys(got))
	}
}

func TestParseAllowedRules_MultipleNamesAndLines(t *testing.T) {
	text := "intro\n" +
		"pilot-guardrail-allow: loc-400, forbidden-import\n" +
		"middle\n" +
		"pilot-guardrail-allow: gateway-auth\n"
	got := parseAllowedRules(text)
	want := []string{"forbidden-import", "gateway-auth", "loc-400"}
	if !reflect.DeepEqual(keys(got), want) {
		t.Errorf("got %v, want %v", keys(got), want)
	}
}

func TestParseAllowedRules_InHTMLComment(t *testing.T) {
	// A directive embedded in an HTML comment must not capture the "-->".
	got := parseAllowedRules("<!-- pilot-guardrail-allow: loc-400 -->")
	if !reflect.DeepEqual(keys(got), []string{"loc-400"}) {
		t.Errorf("html-comment directive must yield clean name, got %v", keys(got))
	}
}

func TestParseAllowedRules_TrailingRemarkStripped(t *testing.T) {
	got := parseAllowedRules("pilot-guardrail-allow: loc-400 # temporary, see GH-1")
	if !reflect.DeepEqual(keys(got), []string{"loc-400"}) {
		t.Errorf("trailing # remark must be stripped, got %v", keys(got))
	}
}

func TestParseAllowedRules_BackticksTrimmed(t *testing.T) {
	got := parseAllowedRules("pilot-guardrail-allow: `loc-400`")
	if !reflect.DeepEqual(keys(got), []string{"loc-400"}) {
		t.Errorf("markdown backticks must be trimmed, got %v", keys(got))
	}
}

func TestParseAllowedRules_InlineWithinSentence(t *testing.T) {
	got := parseAllowedRules("I am waiving this: pilot-guardrail-allow: loc-400 because reasons")
	// "because" and "reasons" are also captured as names; only loc-400 will
	// match a real rule, so over-capture here is harmless. Assert loc-400 is in.
	if !got["loc-400"] {
		t.Errorf("inline directive must still capture loc-400, got %v", keys(got))
	}
}

func violation(rule, file string) architect.Violation {
	return architect.Violation{Rule: rule, File: file, Detail: "d", Risk: pilotapi.RiskLow}
}

func TestPartitionViolations_NoExceptions(t *testing.T) {
	v := []architect.Violation{violation("loc-400", "a.go"), violation("forbidden-import", "b.go")}
	enforced, excepted, used := partitionViolations(v, nil)
	if len(enforced) != 2 || excepted != nil || used != nil {
		t.Errorf("no allow set => everything enforced; got enforced=%d excepted=%v used=%v", len(enforced), excepted, used)
	}
}

func TestPartitionViolations_SuppressesAllowedRule(t *testing.T) {
	v := []architect.Violation{
		violation("loc-400", "a.go"),
		violation("loc-400", "b.go"),
		violation("forbidden-import", "c.go"),
	}
	enforced, excepted, used := partitionViolations(v, map[string]bool{"loc-400": true})
	if len(enforced) != 1 || enforced[0].Rule != "forbidden-import" {
		t.Errorf("only forbidden-import must remain enforced, got %+v", enforced)
	}
	if len(excepted) != 2 {
		t.Errorf("both loc-400 findings must be excepted, got %+v", excepted)
	}
	if !reflect.DeepEqual(used, []string{"loc-400"}) {
		t.Errorf("usedExceptions must be [loc-400], got %v", used)
	}
}

func TestPartitionViolations_AllowedButRuleDidNotFire(t *testing.T) {
	// Allowing a rule that produced no violations is a no-op: nothing excepted,
	// no used-exception recorded.
	v := []architect.Violation{violation("forbidden-import", "c.go")}
	enforced, excepted, used := partitionViolations(v, map[string]bool{"loc-400": true})
	if len(enforced) != 1 {
		t.Errorf("the firing rule must stay enforced, got %+v", enforced)
	}
	if excepted != nil || used != nil {
		t.Errorf("allowing a non-firing rule must record no exception, got excepted=%v used=%v", excepted, used)
	}
}

func TestPartitionViolations_AllAllowed(t *testing.T) {
	v := []architect.Violation{violation("loc-400", "a.go"), violation("gateway-auth", "b.go")}
	enforced, excepted, used := partitionViolations(v, map[string]bool{"loc-400": true, "gateway-auth": true})
	if len(enforced) != 0 {
		t.Errorf("all rules allowed => nothing enforced, got %+v", enforced)
	}
	if len(excepted) != 2 {
		t.Errorf("both must be excepted, got %+v", excepted)
	}
	if !reflect.DeepEqual(used, []string{"gateway-auth", "loc-400"}) {
		t.Errorf("usedExceptions must be sorted union, got %v", used)
	}
}

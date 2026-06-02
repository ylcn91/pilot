package architect

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestExtractJSONCleanArray(t *testing.T) {
	in := `[{"title":"x"}]`
	got := extractJSON(in)
	if got != in {
		t.Errorf("extractJSON(clean) = %q, want %q", got, in)
	}
}

func TestExtractJSONFenced(t *testing.T) {
	in := "```json\n[{\"title\":\"x\"}]\n```"
	got := extractJSON(in)
	want := `[{"title":"x"}]`
	if got != want {
		t.Errorf("extractJSON(fenced) = %q, want %q", got, want)
	}
}

func TestExtractJSONFencedNoLang(t *testing.T) {
	in := "```\n[1,2,3]\n```"
	if got := extractJSON(in); got != "[1,2,3]" {
		t.Errorf("extractJSON(fenced no lang) = %q, want %q", got, "[1,2,3]")
	}
}

func TestExtractJSONSurroundingProse(t *testing.T) {
	in := "Here are the proposals you asked for:\n[{\"title\":\"x\"}]\nLet me know if you need more."
	want := `[{"title":"x"}]`
	if got := extractJSON(in); got != want {
		t.Errorf("extractJSON(prose) = %q, want %q", got, want)
	}
}

func TestExtractJSONProseAndFence(t *testing.T) {
	in := "Sure!\n```json\n[{\"title\":\"x\"}]\n```\nDone."
	want := `[{"title":"x"}]`
	if got := extractJSON(in); got != want {
		t.Errorf("extractJSON(prose+fence) = %q, want %q", got, want)
	}
}

func TestExtractJSONTrailingTextAfterArray(t *testing.T) {
	// LastIndexByte must pick the array close, not stop at the first ].
	in := `[{"files":["a"]},{"files":["b"]}] and that's all`
	want := `[{"files":["a"]},{"files":["b"]}]`
	if got := extractJSON(in); got != want {
		t.Errorf("extractJSON(trailing) = %q, want %q", got, want)
	}
}

func TestExtractJSONNoArray(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"no json here at all",
		"{\"title\":\"object not array\"}", // object braces, no brackets
		"]only a close[",                   // close before open
	} {
		if got := extractJSON(in); got != "" {
			t.Errorf("extractJSON(%q) = %q, want empty", in, got)
		}
	}
}

func TestStripCodeFencesPreservesContent(t *testing.T) {
	in := "```json\nline1\nline2\n```"
	got := stripCodeFences(in)
	if strings.Contains(got, "```") {
		t.Errorf("stripCodeFences left a fence: %q", got)
	}
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Errorf("stripCodeFences dropped content: %q", got)
	}
}

func TestStripCodeFencesNoFence(t *testing.T) {
	in := "plain text\nno fences"
	if got := stripCodeFences(in); got != in {
		t.Errorf("stripCodeFences(no fence) = %q, want unchanged", got)
	}
}

func TestNormalizeRisk(t *testing.T) {
	tests := []struct {
		name string
		in   pilotapi.RiskLevel
		want pilotapi.RiskLevel
	}{
		{"low", "low", pilotapi.RiskLow},
		{"high", "high", pilotapi.RiskHigh},
		{"release-blocker", "release-blocker", pilotapi.RiskReleaseBlocker},
		{"uppercase normalized", "HIGH", pilotapi.RiskHigh},
		{"whitespace normalized", "  medium  ", pilotapi.RiskMedium},
		{"empty defaults medium", "", pilotapi.RiskMedium},
		{"unknown defaults medium", "critical", pilotapi.RiskMedium},
		{"garbage defaults medium", "!!!", pilotapi.RiskMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRisk(tt.in)
			if got != tt.want {
				t.Errorf("normalizeRisk(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !got.IsValid() {
				t.Errorf("normalizeRisk(%q) returned non-valid risk %q", tt.in, got)
			}
		})
	}
}

func TestProposalJSONShapeMentionsAllFields(t *testing.T) {
	for _, field := range []string{
		"title", "kind", "risk", "why_it_matters",
		"suggested_pr_pieces", "test_plan", "files",
	} {
		if !strings.Contains(proposalJSONShape, field) {
			t.Errorf("proposalJSONShape missing field %q", field)
		}
	}
}

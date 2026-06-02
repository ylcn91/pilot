package architect

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestProposalTitle_WrapsNonConventional(t *testing.T) {
	f := finding("make the thing faster", "perf", pilotapi.RiskMedium, "internal/api/server.go")
	got := proposalTitle(f)
	if !strings.HasPrefix(got, "refactor(internal): ") {
		t.Fatalf("title = %q, want refactor(internal): prefix from first file segment", got)
	}
	if !titleHasConventionalPrefix.MatchString(got) {
		t.Errorf("wrapped title %q must satisfy conventional-commit format", got)
	}
}

func TestProposalTitle_PassesConventionalThrough(t *testing.T) {
	f := finding("refactor(scan): split collector", "refactor", pilotapi.RiskHigh, "a.go")
	if got := proposalTitle(f); got != "refactor(scan): split collector" {
		t.Fatalf("an already-conventional title must pass through verbatim, got %q", got)
	}
}

func TestProposalTitle_TopLevelFileScope(t *testing.T) {
	f := finding("tidy", "refactor", pilotapi.RiskLow, "main.go")
	if got := proposalTitle(f); !strings.HasPrefix(got, "refactor(main): ") {
		t.Fatalf("top-level file scope should drop the extension, got %q", got)
	}
}

func TestProposalTitle_NoFilesFallbackScope(t *testing.T) {
	f := finding("do a thing", "refactor", pilotapi.RiskLow)
	if got := proposalTitle(f); !strings.HasPrefix(got, "refactor(architect): ") {
		t.Fatalf("a fileless proposal must fall back to the architect scope, got %q", got)
	}
}

func TestScopeFromFiles_Sanitises(t *testing.T) {
	if got := scopeFromFiles([]string{"weird name!/x.go"}); got != "weirdname" {
		t.Errorf("scope = %q, want sanitised weirdname", got)
	}
	if got := scopeFromFiles([]string{"  ", "", "pkg/sub/file.go"}); got != "pkg" {
		t.Errorf("scope should skip blanks and use first real file, got %q", got)
	}
	if got := scopeFromFiles(nil); got != "architect" {
		t.Errorf("nil files → architect, got %q", got)
	}
}

func TestProposalBody_ContainsAllSections(t *testing.T) {
	f := pilotapi.Finding{
		Title:             "Split scanner",
		Kind:              "refactor",
		Risk:              pilotapi.RiskHigh,
		WhyItMatters:      "the file is too long",
		SuggestedPRPieces: []string{"extract collectors", "extract aggregation"},
		TestPlan:          "run go test ./internal/architect/...",
		Files:             []string{"internal/architect/scanner.go"},
	}
	marker := MarkerFor("deadbeef")
	body := proposalBody(f, marker)

	for _, want := range []string{
		"the file is too long",
		"## Suggested PR pieces",
		"- extract collectors",
		"- extract aggregation",
		"## Test plan",
		"run go test",
		"## Files",
		"`internal/architect/scanner.go`",
		"**Risk:** high",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestProposalBody_MarkerIsLastLine(t *testing.T) {
	f := finding("x", "refactor", pilotapi.RiskHigh, "a.go")
	marker := MarkerFor("abc123")
	body := proposalBody(f, marker)
	if !strings.Contains(body, marker) {
		t.Fatal("body must embed the dedup marker")
	}
	trimmed := strings.TrimRight(body, "\n")
	lines := strings.Split(trimmed, "\n")
	if last := lines[len(lines)-1]; last != marker {
		t.Errorf("marker must be the last content line, got %q", last)
	}
}

func TestProposalBody_OmitsEmptySections(t *testing.T) {
	f := pilotapi.Finding{
		Title: "bare",
		Kind:  "refactor",
		Risk:  pilotapi.RiskMedium,
		// No why/pieces/test-plan/files.
	}
	body := proposalBody(f, MarkerFor("h"))
	for _, absent := range []string{"## Why it matters", "## Suggested PR pieces", "## Test plan", "## Files"} {
		if strings.Contains(body, absent) {
			t.Errorf("empty section %q must be omitted:\n%s", absent, body)
		}
	}
}

func TestProposalBody_EmptyRiskDefaultsToMedium(t *testing.T) {
	f := pilotapi.Finding{Title: "x", Kind: "refactor"}
	body := proposalBody(f, MarkerFor("h"))
	if !strings.Contains(body, "**Risk:** medium") {
		t.Errorf("empty risk should render as medium, got:\n%s", body)
	}
}

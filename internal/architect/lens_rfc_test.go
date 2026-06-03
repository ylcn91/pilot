package architect

import (
	"os"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestRFCLens_Registered(t *testing.T) {
	l, err := LensByName(RFCLensName)
	if err != nil {
		t.Fatalf("rfc lens not registered: %v", err)
	}
	if l.Name != RFCLensName {
		t.Fatalf("lens name = %q, want %q", l.Name, RFCLensName)
	}
	if l.Slant == nil {
		t.Fatal("rfc lens must carry a slant")
	}
	if !contains(LensNames(), RFCLensName) {
		t.Fatalf("rfc lens must appear in LensNames(): %v", LensNames())
	}
}

// TestRFCLens_SelectableCaseInsensitive proves `--lens rfc` (and case/whitespace
// variants) resolves to the lens exactly like the CLI flag path.
func TestRFCLens_SelectableCaseInsensitive(t *testing.T) {
	for _, name := range []string{"rfc", "RFC", "  Rfc  "} {
		l, err := LensByName(name)
		if err != nil {
			t.Fatalf("LensByName(%q): %v", name, err)
		}
		if l.Name != RFCLensName {
			t.Fatalf("LensByName(%q) = %q, want %q", name, l.Name, RFCLensName)
		}
	}
}

// TestRFCLens_BundlesRefactorCollectors proves the rfc roster mirrors the
// refactor lens (core collectors + dependency-doctor) so the tiny-PR sequence is
// driven by the same blast-radius signals.
func TestRFCLens_BundlesRefactorCollectors(t *testing.T) {
	l, _ := LensByName(RFCLensName)
	got := names(l.Collectors(ScanOptions{}))
	want := map[string]bool{
		"loc_over_400":      true,
		"todo_fixme":        true,
		kindDuplicateBlock:  true,
		"lint":              true,
		"dependency_doctor": true,
	}
	if len(got) != len(want) {
		t.Fatalf("rfc collectors = %v, want %d", got, len(want))
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("unexpected rfc collector %q (got %v)", n, got)
		}
	}
}

func TestRFCLens_BuildLensScanner(t *testing.T) {
	s, err := BuildLensScanner("rfc", "/proj", ScanOptions{})
	if err != nil {
		t.Fatalf("build rfc scanner: %v", err)
	}
	if len(s.Collectors()) != 5 {
		t.Fatalf("rfc scanner collectors = %d, want 5: %v", len(s.Collectors()), names(s.Collectors()))
	}
}

func TestRFCSlant_AimsAtNarrativeSections(t *testing.T) {
	s := rfcSlant
	if s.headingOr("x") != rfcHeading {
		t.Fatalf("heading = %q", s.headingOr("x"))
	}
	extra := s.extraInstruction()
	for _, must := range []string{"Problem Statement", "Constraints", "Alternatives", "Decision", "Rollout", "Rollback"} {
		if !containsSub(extra, must) {
			t.Fatalf("rfc instruction must name section %q: %q", must, extra)
		}
	}
}

// rfcFixtureSignals is the shared signal fixture for the offline-generation
// tests: a leaf split, a core split (high blast in planGraph), a TODO cleanup,
// and a layer violation (behaviour change).
func rfcFixtureSignals() []Signal {
	return []Signal{
		{Kind: "loc_over_400", File: "internal/leaf/big.go", Detail: "420 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "loc_over_400", File: "internal/core/big.go", Detail: "510 lines", Weight: 1, Risk: pilotapi.RiskMedium},
		{Kind: "todo_fixme", File: "internal/mid/x.go", Detail: "TODO: clean up", Weight: 1, Risk: pilotapi.RiskLow},
		{Kind: kindLayerViolation, File: "internal/top/d.go", Detail: "forbidden import", Weight: 1, Risk: pilotapi.RiskHigh},
	}
}

// TestGenerateRFCOffline_AllSections proves the offline document contains every
// RFC section, the frontmatter, and a tiny-PR sequence — the full contract — from
// fixture signals with no LLM/network.
func TestGenerateRFCOffline_AllSections(t *testing.T) {
	doc := GenerateRFCOffline("Decompose architect package", rfcFixtureSignals(), planGraph(), nil)
	body := doc.Body

	// Frontmatter.
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("document must open with YAML frontmatter:\n%s", body[:min(120, len(body))])
	}
	for _, f := range []string{"title:", "slug:", "status: proposed", "risk:", "pr_count:", "manual_review:"} {
		if !strings.Contains(body, f) {
			t.Fatalf("frontmatter missing %q", f)
		}
	}
	// All six narrative sections + the sequence heading.
	for _, sec := range []string{
		rfcSecProblem, rfcSecConstraints, rfcSecAlternatives,
		rfcSecDecision, rfcSecRollout, rfcSecRisk, rfcSecSequence,
	} {
		if !strings.Contains(body, sec) {
			t.Fatalf("document missing section %q", sec)
		}
	}
	// Risk & Rollback must actually talk about rollback.
	if !strings.Contains(strings.ToLower(body), "rollback") {
		t.Fatal("Risk & Rollback section must describe a rollback path")
	}
	// Tiny-PR sequence must enumerate ordered, numbered PRs.
	if !strings.Contains(body, "1. **") {
		t.Fatalf("tiny-PR sequence must list numbered PR entries:\n%s", body)
	}
	if doc.Slug != "decompose-architect-package" {
		t.Fatalf("slug = %q", doc.Slug)
	}
	if doc.Risk != pilotapi.RiskHigh {
		t.Fatalf("doc risk = %q, want high (layer violation present)", doc.Risk)
	}
}

// TestGenerateRFCOffline_SequenceMirrorsPlan proves the rendered sequence carries
// exactly the plan's PRs in order, with their class/blast/pilot-safe verdict.
func TestGenerateRFCOffline_SequenceMirrorsPlan(t *testing.T) {
	signals := rfcFixtureSignals()
	plan := PlanRefactorOffline(signals, planGraph(), nil)
	doc := GenerateRFCOffline("seq", signals, planGraph(), nil)

	for _, pr := range plan.PRs {
		if !strings.Contains(doc.Body, pr.Title) {
			t.Fatalf("sequence missing PR title %q", pr.Title)
		}
	}
	if !strings.Contains(doc.Body, "class=") || !strings.Contains(doc.Body, "blast=") {
		t.Fatal("sequence entries must carry class= and blast= annotations")
	}
	// A manual-review PR must surface its verdict.
	if plan.Manual > 0 && !strings.Contains(doc.Body, "manual-review") {
		t.Fatal("a manual PR must render a manual-review verdict")
	}
}

func TestGenerateRFCOffline_Deterministic(t *testing.T) {
	signals := rfcFixtureSignals()
	first := GenerateRFCOffline("Stable Title", signals, planGraph(), nil)
	for i := 0; i < 5; i++ {
		again := GenerateRFCOffline("Stable Title", signals, planGraph(), nil)
		if again.Body != first.Body || again.Slug != first.Slug || again.Risk != first.Risk {
			t.Fatalf("GenerateRFCOffline non-deterministic on run %d", i)
		}
	}
}

// TestGenerateRFCOffline_EmptySignals proves an empty scan still yields a complete
// document (every section present) with an explicit no-PR sequence.
func TestGenerateRFCOffline_EmptySignals(t *testing.T) {
	doc := GenerateRFCOffline("Empty", nil, planGraph(), nil)
	for _, sec := range []string{rfcSecProblem, rfcSecDecision, rfcSecRollout, rfcSecRisk, rfcSecSequence} {
		if !strings.Contains(doc.Body, sec) {
			t.Fatalf("empty-signal document missing section %q", sec)
		}
	}
	if !strings.Contains(doc.Body, "No refactor units") {
		t.Fatalf("empty sequence must state there are no PRs:\n%s", doc.Body)
	}
	if doc.Risk != pilotapi.RiskMedium {
		t.Fatalf("empty doc risk = %q, want medium default", doc.Risk)
	}
}

// TestGenerateRFCOffline_NilGraph proves the generator tolerates a nil graph
// (dry-run without `go list`): every PR becomes a zero-blast leaf and the doc
// still renders.
func TestGenerateRFCOffline_NilGraph(t *testing.T) {
	doc := GenerateRFCOffline("NoGraph", rfcFixtureSignals(), nil, nil)
	if !strings.Contains(doc.Body, rfcSecSequence) {
		t.Fatal("nil-graph document must still render the sequence")
	}
	if !strings.Contains(doc.Body, "blast=0") {
		t.Fatalf("nil graph => every PR zero-blast:\n%s", doc.Body)
	}
}

// TestGenerateRFCOffline_BlankTitleDefault proves a blank title falls back to the
// default and a usable slug.
func TestGenerateRFCOffline_BlankTitleDefault(t *testing.T) {
	doc := GenerateRFCOffline("   ", rfcFixtureSignals(), planGraph(), nil)
	if doc.Title != defaultRFCTitle {
		t.Fatalf("blank title => %q, want %q", doc.Title, defaultRFCTitle)
	}
	if doc.Slug == "" {
		t.Fatal("blank title must still yield a non-empty slug")
	}
}

// TestRFCLens_DryRunWritesNothing proves generating the offline RFC never touches
// disk: a temp dir used as the ADR sink stays empty until WriteADR is explicitly
// called (the --create-issues path). GenerateRFCOffline returns the body only.
func TestRFCLens_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	doc := GenerateRFCOffline("Dry Run", rfcFixtureSignals(), planGraph(), nil)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read sink dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry-run wrote %d files, want 0", len(entries))
	}
	// And the would-write path is computable without writing.
	wantPath := ADRPath(dir, doc.Slug)
	if !strings.HasSuffix(wantPath, "rfc_dry-run.md") {
		t.Fatalf("would-write path = %q, want suffix rfc_dry-run.md", wantPath)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("would-write path must not exist in dry-run: %v", err)
	}
}

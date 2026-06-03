package autopilot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
)

// writeFixture writes content to dir/rel (forward-slash rel), creating parents.
func writeFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func bigGoFile(pkg string, lines int) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n")
	for i := 1; i < lines; i++ {
		b.WriteString("// filler line\n")
	}
	return b.String()
}

// This is the no-stub proof: the gate runs the REAL DefaultRuleRegistry over a
// real fixture worktree. The rules genuinely read the planted files (counting
// LOC, parsing forbidden imports) and the gate genuinely posts via the GitHub
// client interface — both ends asserted, neither faked.
func TestGuardrailsGate_RealRules_OverFixtureWorktree(t *testing.T) {
	wt := t.TempDir()

	// 1. An oversized file => loc-400 violation.
	writeFixture(t, wt, "internal/feature/huge.go", bigGoFile("feature", 450))
	// 2. A forbidden import edge: executor importing config.
	writeFixture(t, wt, "internal/executor/bad.go",
		"package executor\n\nimport \"github.com/ylcn91/pilot/internal/config\"\n\nvar _ = config.DefaultConfig\n")
	// 3. A clean small file that must NOT be flagged.
	writeFixture(t, wt, "internal/feature/ok.go", "package feature\n\nfunc Ok() {}\n")

	changed := prFiles(
		"internal/feature/huge.go",
		"internal/executor/bad.go",
		"internal/feature/ok.go",
	)
	gh := &mockGuardrailsGH{files: changed}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")

	g := NewGuardrailsGate(gh, registry,
		GuardrailsGateConfig{Enabled: true, Mode: "block"}, wt, "owner", "repo")

	v, err := g.Evaluate(context.Background(), 99, "realsha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Real rules must have produced exactly the two planted violations.
	if len(v) != 2 {
		t.Fatalf("expected 2 real violations (loc-400 + forbidden-import), got %d: %+v", len(v), v)
	}
	rules := map[string]bool{}
	for _, vio := range v {
		rules[vio.Rule] = true
	}
	if !rules["loc-400"] {
		t.Error("real loc-400 rule must have fired on the 450-line file")
	}
	if !rules["forbidden-import"] {
		t.Error("real forbidden-import rule must have fired on executor->config")
	}

	// Block mode + violations => a real FAILURE status posted via the client.
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "failure" {
		t.Fatalf("block mode must post a failure status, got %+v", gh.statusCalls)
	}
	if len(gh.commentBody) != 1 {
		t.Fatalf("a comment must be posted, got %d", len(gh.commentBody))
	}
	body := gh.commentBody[0]
	if !strings.Contains(body, "internal/feature/huge.go") ||
		!strings.Contains(body, "internal/executor/bad.go") {
		t.Errorf("comment must name both offending files, got:\n%s", body)
	}
	if strings.Contains(body, "internal/feature/ok.go") {
		t.Errorf("clean file must not appear in the comment, got:\n%s", body)
	}
}

// Report mode over the same real rules: violations exist but the status stays
// green — proving report-only never blocks even when real rules fire.
func TestGuardrailsGate_RealRules_ReportModeStaysGreen(t *testing.T) {
	wt := t.TempDir()
	writeFixture(t, wt, "internal/feature/huge.go", bigGoFile("feature", 500))

	gh := &mockGuardrailsGH{files: prFiles("internal/feature/huge.go")}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
	g := NewGuardrailsGate(gh, registry,
		GuardrailsGateConfig{Enabled: true, Mode: "report"}, wt, "owner", "repo")

	v, err := g.Evaluate(context.Background(), 100, "realsha2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) == 0 {
		t.Fatal("real loc-400 rule should have fired on a 500-line file")
	}
	if gh.statusCalls[0].State != "success" {
		t.Errorf("report mode must keep status green despite real violations, got %q", gh.statusCalls[0].State)
	}
}

// A disabled-rule list must actually suppress a real rule end-to-end.
func TestGuardrailsGate_RealRules_DisabledRuleSuppressed(t *testing.T) {
	wt := t.TempDir()
	writeFixture(t, wt, "internal/feature/huge.go", bigGoFile("feature", 450))

	gh := &mockGuardrailsGH{files: prFiles("internal/feature/huge.go")}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
	g := NewGuardrailsGate(gh, registry, GuardrailsGateConfig{
		Enabled:       true,
		Mode:          "block",
		DisabledRules: []string{"loc-400"},
	}, wt, "owner", "repo")

	v, err := g.Evaluate(context.Background(), 101, "realsha3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("disabling loc-400 must suppress the only violation, got %+v", v)
	}
	if gh.statusCalls[0].State != "success" {
		t.Errorf("no effective violations => status must be green, got %q", gh.statusCalls[0].State)
	}
	if len(gh.commentBody) != 0 {
		t.Errorf("no violations => no comment, got %+v", gh.commentBody)
	}
}

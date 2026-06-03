package architect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

const testModule = "github.com/ylcn91/pilot"

// writeRelFile writes content to dir/rel (rel uses forward slashes), creating
// parent dirs.
func writeRelFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// goFileOfLines builds a syntactically-trivial Go file with the given line count.
func goFileOfLines(pkg string, lines int) string {
	var sb strings.Builder
	sb.WriteString("package " + pkg + "\n")
	for i := 1; i < lines; i++ {
		sb.WriteString("// line\n")
	}
	return sb.String()
}

// --- Registry ---------------------------------------------------------------

func TestRuleRegistry_ListAndLookup(t *testing.T) {
	r := DefaultRuleRegistry(testModule)
	names := r.Names()
	want := []string{"loc-400", "forbidden-import", "gateway-auth"}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Names()[%d] = %q, want %q", i, names[i], n)
		}
		if _, ok := r.Lookup(n); !ok {
			t.Errorf("Lookup(%q) not found", n)
		}
	}
	if _, ok := r.Lookup("does-not-exist"); ok {
		t.Error("Lookup of unknown rule should be false")
	}
	if got := len(r.Rules()); got != 3 {
		t.Errorf("Rules() len = %d, want 3", got)
	}
}

func TestRuleRegistry_RulesIsCopy(t *testing.T) {
	r := DefaultRuleRegistry(testModule)
	rules := r.Rules()
	rules[0] = nil
	if r.Rules()[0] == nil {
		t.Error("Rules() must return a copy; mutation leaked into registry")
	}
}

func TestRuleRegistry_EvaluateDisabledRuleSkipped(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/big.go", goFileOfLines("x", 450))

	r := DefaultRuleRegistry(testModule)
	with := r.Evaluate(context.Background(), []string{"internal/x/big.go"}, dir, nil)
	if len(with) != 1 || with[0].Rule != "loc-400" {
		t.Fatalf("expected one loc-400 violation, got %+v", with)
	}
	without := r.Evaluate(context.Background(), []string{"internal/x/big.go"}, dir, []string{"loc-400"})
	if len(without) != 0 {
		t.Fatalf("disabled rule must produce no violations, got %+v", without)
	}
}

func TestRuleRegistry_EvaluateSortedAndStable(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/z/big.go", goFileOfLines("z", 450))
	writeRelFile(t, dir, "internal/a/big.go", goFileOfLines("a", 450))

	r := DefaultRuleRegistry(testModule)
	got := r.Evaluate(context.Background(), []string{"internal/z/big.go", "internal/a/big.go"}, dir, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 violations, got %+v", got)
	}
	if got[0].File != "internal/a/big.go" || got[1].File != "internal/z/big.go" {
		t.Errorf("violations not sorted by file: %+v", got)
	}
}

func TestRuleRegistry_EvaluateEmptyChangedFiles(t *testing.T) {
	r := DefaultRuleRegistry(testModule)
	if got := r.Evaluate(context.Background(), nil, t.TempDir(), nil); len(got) != 0 {
		t.Errorf("nil changed files must yield no violations, got %+v", got)
	}
}

func TestRuleRegistry_EvaluateCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/big.go", goFileOfLines("x", 450))
	r := DefaultRuleRegistry(testModule)
	if got := r.Evaluate(ctx, []string{"internal/x/big.go"}, dir, nil); len(got) != 0 {
		t.Errorf("cancelled context must short-circuit, got %+v", got)
	}
}

func TestNewRuleRegistry_DuplicateNameLastWinsLookup(t *testing.T) {
	r := NewRuleRegistry(NewLOCRule(), NewLOCRule())
	if got := len(r.Rules()); got != 2 {
		t.Errorf("ordered list keeps both, len = %d", got)
	}
	if _, ok := r.Lookup("loc-400"); !ok {
		t.Error("lookup should resolve duplicate name")
	}
}

// --- LOCRule ----------------------------------------------------------------

func TestLOCRule_FlagsOversizedChangedFile(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/big.go", goFileOfLines("x", 450))
	writeRelFile(t, dir, "internal/x/small.go", goFileOfLines("x", 50))

	v := NewLOCRule().Eval(context.Background(),
		[]string{"internal/x/big.go", "internal/x/small.go"}, dir)
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %+v", v)
	}
	if v[0].Rule != "loc-400" || v[0].File != "internal/x/big.go" {
		t.Errorf("unexpected violation: %+v", v[0])
	}
	if v[0].Risk != pilotapi.RiskMedium {
		t.Errorf("450-line file should be medium risk, got %q", v[0].Risk)
	}
}

func TestLOCRule_HighRiskForVeryLargeFile(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/huge.go", goFileOfLines("x", 700))
	v := NewLOCRule().Eval(context.Background(), []string{"internal/x/huge.go"}, dir)
	if len(v) != 1 || v[0].Risk != pilotapi.RiskHigh {
		t.Fatalf("700-line file should be high risk, got %+v", v)
	}
}

func TestLOCRule_ExactThresholdFlagged(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/edge.go", goFileOfLines("x", LOCThreshold))
	v := NewLOCRule().Eval(context.Background(), []string{"internal/x/edge.go"}, dir)
	if len(v) != 1 {
		t.Fatalf("file at exactly the threshold must be flagged, got %+v", v)
	}
}

func TestLOCRule_JustUnderThresholdSilent(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/x/edge.go", goFileOfLines("x", LOCThreshold-1))
	v := NewLOCRule().Eval(context.Background(), []string{"internal/x/edge.go"}, dir)
	if len(v) != 0 {
		t.Fatalf("file under threshold must be silent, got %+v", v)
	}
}

func TestLOCRule_IgnoresNonGoAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "docs/big.md", strings.Repeat("x\n", 500))
	v := NewLOCRule().Eval(context.Background(),
		[]string{"docs/big.md", "internal/x/deleted.go"}, dir)
	if len(v) != 0 {
		t.Fatalf("non-go and missing files must not flag, got %+v", v)
	}
}

func TestLOCRule_EmptyWorktreeIsBestEffort(t *testing.T) {
	v := NewLOCRule().Eval(context.Background(), []string{"internal/x/big.go"}, "")
	if v != nil {
		t.Errorf("empty worktree must yield nil, got %+v", v)
	}
}

// --- ForbiddenImportRule ----------------------------------------------------

func TestForbiddenImportRule_FlagsExecutorImportingConfig(t *testing.T) {
	dir := t.TempDir()
	src := "package executor\n\nimport (\n\t\"github.com/ylcn91/pilot/internal/config\"\n)\n\nvar _ = config.DefaultConfig\n"
	writeRelFile(t, dir, "internal/executor/bad.go", src)

	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	v := r.Eval(context.Background(), []string{"internal/executor/bad.go"}, dir)
	if len(v) != 1 {
		t.Fatalf("expected 1 forbidden-import violation, got %+v", v)
	}
	if v[0].Rule != "forbidden-import" || v[0].Risk != pilotapi.RiskHigh {
		t.Errorf("unexpected violation: %+v", v[0])
	}
	if !strings.Contains(v[0].Detail, "internal/config") {
		t.Errorf("detail should name the forbidden import, got %q", v[0].Detail)
	}
}

func TestForbiddenImportRule_NoFalsePositiveOnLookalikePath(t *testing.T) {
	dir := t.TempDir()
	// internal/configx is NOT internal/config; must not trip the rule.
	src := "package executor\n\nimport (\n\t\"github.com/ylcn91/pilot/internal/configx\"\n)\n\nvar _ = configx.X\n"
	writeRelFile(t, dir, "internal/executor/ok.go", src)

	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	v := r.Eval(context.Background(), []string{"internal/executor/ok.go"}, dir)
	if len(v) != 0 {
		t.Fatalf("lookalike path internal/configx must not be flagged, got %+v", v)
	}
}

func TestForbiddenImportRule_AllowsCleanImports(t *testing.T) {
	dir := t.TempDir()
	src := "package executor\n\nimport (\n\t\"fmt\"\n\t\"github.com/ylcn91/pilot/internal/pilotapi\"\n)\n\nvar _ = fmt.Sprint\nvar _ pilotapi.RiskLevel\n"
	writeRelFile(t, dir, "internal/executor/clean.go", src)

	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	if v := r.Eval(context.Background(), []string{"internal/executor/clean.go"}, dir); len(v) != 0 {
		t.Fatalf("clean executor file must not be flagged, got %+v", v)
	}
}

func TestForbiddenImportRule_PilotapiLeafViolation(t *testing.T) {
	dir := t.TempDir()
	src := "package pilotapi\n\nimport (\n\t\"github.com/ylcn91/pilot/internal/executor\"\n)\n\nvar _ = executor.X\n"
	writeRelFile(t, dir, "internal/pilotapi/bad.go", src)

	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	v := r.Eval(context.Background(), []string{"internal/pilotapi/bad.go"}, dir)
	if len(v) != 1 {
		t.Fatalf("pilotapi importing another internal pkg must be flagged, got %+v", v)
	}
}

func TestForbiddenImportRule_IgnoresUnparseableFile(t *testing.T) {
	dir := t.TempDir()
	writeRelFile(t, dir, "internal/executor/broken.go", "this is not valid go @@@")
	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	if v := r.Eval(context.Background(), []string{"internal/executor/broken.go"}, dir); len(v) != 0 {
		t.Fatalf("unparseable file must be skipped, got %+v", v)
	}
}

func TestForbiddenImportRule_EmptyWorktreeIsBestEffort(t *testing.T) {
	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	if v := r.Eval(context.Background(), []string{"internal/executor/x.go"}, ""); v != nil {
		t.Errorf("empty worktree must yield nil, got %+v", v)
	}
}

func TestForbiddenImportRule_UnrelatedPackageNotFlagged(t *testing.T) {
	dir := t.TempDir()
	// memory importing config is fine; the rule only forbids executor->config.
	src := "package memory\n\nimport (\n\t\"github.com/ylcn91/pilot/internal/config\"\n)\n\nvar _ = config.DefaultConfig\n"
	writeRelFile(t, dir, "internal/memory/ok.go", src)
	r := NewForbiddenImportRule(testModule, defaultLayerRules)
	if v := r.Eval(context.Background(), []string{"internal/memory/ok.go"}, dir); len(v) != 0 {
		t.Fatalf("memory->config is allowed, got %+v", v)
	}
}

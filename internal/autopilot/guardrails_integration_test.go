package autopilot

import (
	"context"
	"os"
	"os/exec"
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

// runGit runs a git command in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// gitHeadSHA returns the current HEAD SHA of the repo at dir.
func gitHeadSHA(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// commitFiles writes each path/content pair into the repo, stages them, and
// commits with the given message.
func commitFiles(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		writeFixture(t, dir, rel, content)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
}

// This is the regression test for the content-source bug: the rules used to read
// the static checkout (main) instead of the PR head, so loc-400 was judged
// against main's clean content and net-new PR files (which don't exist on main)
// were silently skipped. With Option A (materialise the PR head per run) the gate
// must flag the PR-head content even though main is clean.
func TestGuardrailsGate_PRHeadContent_FlagsPRNotMain(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")

	// main: a single small file, well under the LOC limit, and nothing else.
	commitFiles(t, repo, "seed main", map[string]string{
		"internal/feature/grows.go": bigGoFile("feature", 50),
	})

	// PR head: grow the existing file past the limit AND add a net-new oversized
	// file that does not exist on main at all.
	runGit(t, repo, "checkout", "-q", "-b", "pr-head")
	commitFiles(t, repo, "pr head: grow + net-new", map[string]string{
		"internal/feature/grows.go":  bigGoFile("feature", 450),
		"internal/feature/netnew.go": bigGoFile("feature", 500),
	})
	headSHA := gitHeadSHA(t, repo)

	// Leave the clone checked out on MAIN: if the gate read the static checkout
	// (the old bug) it would see only the 50-line file and flag nothing.
	runGit(t, repo, "checkout", "-q", "main")

	gh := &mockGuardrailsGH{files: prFiles(
		"internal/feature/grows.go",
		"internal/feature/netnew.go",
	)}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
	g := NewGuardrailsGate(gh, registry,
		GuardrailsGateConfig{Enabled: true, Mode: "block"}, repo, "owner", "repo")

	v, err := g.Evaluate(context.Background(), 200, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	files := map[string]bool{}
	for _, vio := range v {
		if vio.Rule == "loc-400" {
			files[vio.File] = true
		}
	}
	if !files["internal/feature/grows.go"] {
		t.Error("loc-400 must fire on the file grown past the limit on the PR head (main is clean)")
	}
	if !files["internal/feature/netnew.go"] {
		t.Error("loc-400 must fire on the net-new oversized file that exists only on the PR head")
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "failure" {
		t.Fatalf("block mode + PR-head violations must post a failure status, got %+v", gh.statusCalls)
	}
}

// Deleted-in-PR files are absent at the head SHA, so git show fails for them and
// they are correctly skipped — they must not error the gate or appear as
// findings.
func TestGuardrailsGate_PRHeadContent_DeletedFileSkipped(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")

	commitFiles(t, repo, "seed main", map[string]string{
		"internal/feature/keep.go": bigGoFile("feature", 30),
		"internal/feature/gone.go": bigGoFile("feature", 30),
	})

	runGit(t, repo, "checkout", "-q", "-b", "pr-head")
	runGit(t, repo, "rm", "-q", "internal/feature/gone.go")
	runGit(t, repo, "commit", "-q", "-m", "pr head: delete gone.go")
	headSHA := gitHeadSHA(t, repo)
	runGit(t, repo, "checkout", "-q", "main")

	gh := &mockGuardrailsGH{files: prFiles(
		"internal/feature/keep.go",
		"internal/feature/gone.go", // deleted on the PR head
	)}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
	g := NewGuardrailsGate(gh, registry,
		GuardrailsGateConfig{Enabled: true, Mode: "block"}, repo, "owner", "repo")

	v, err := g.Evaluate(context.Background(), 201, headSHA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("clean PR-head content must yield no findings, got %+v", v)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "success" {
		t.Fatalf("clean PR must post a green status, got %+v", gh.statusCalls)
	}
}

// Fail-open: a bogus head SHA that cannot be resolved (and cannot be fetched,
// since the repo has no usable origin) must skip the gate entirely — no status,
// no comment, no findings — never block the PR on a fetch failure.
func TestGuardrailsGate_PRHeadContent_BogusSHAFailsOpen(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")
	commitFiles(t, repo, "seed main", map[string]string{
		"internal/feature/grows.go": bigGoFile("feature", 50),
	})

	gh := &mockGuardrailsGH{files: prFiles("internal/feature/grows.go")}
	registry := architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
	g := NewGuardrailsGate(gh, registry,
		GuardrailsGateConfig{Enabled: true, Mode: "block"}, repo, "owner", "repo")

	bogus := "0000000000000000000000000000000000000000"
	v, err := g.Evaluate(context.Background(), 202, bogus)
	if err != nil {
		t.Fatalf("fail-open must swallow the error, got %v", err)
	}
	if v != nil {
		t.Errorf("unresolvable head SHA must yield no findings, got %+v", v)
	}
	if len(gh.statusCalls) != 0 {
		t.Errorf("a fetch failure must post NO status (never block on our own failure), got %+v", gh.statusCalls)
	}
	if len(gh.commentBody) != 0 {
		t.Errorf("a fetch failure must post NO comment, got %+v", gh.commentBody)
	}
}

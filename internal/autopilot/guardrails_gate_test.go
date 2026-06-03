package autopilot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// --- test doubles -----------------------------------------------------------

// mockGuardrailsGH records every call the gate makes so tests can assert that
// the gate posts exactly the status/comment it should — and nothing more.
type mockGuardrailsGH struct {
	files    []*github.PRFile
	filesErr error

	statusErr  error
	commentErr error

	statusCalls  []*github.CommitStatus
	statusSHAs   []string
	commentBody  []string
	commentPRs   []int
	listPRNumber int
	listCalls    int
}

func (m *mockGuardrailsGH) ListPullRequestFiles(_ context.Context, _, _ string, number int) ([]*github.PRFile, error) {
	m.listCalls++
	m.listPRNumber = number
	if m.filesErr != nil {
		return nil, m.filesErr
	}
	return m.files, nil
}

func (m *mockGuardrailsGH) CreateCommitStatus(_ context.Context, _, _, sha string, status *github.CommitStatus) (*github.CommitStatus, error) {
	m.statusCalls = append(m.statusCalls, status)
	m.statusSHAs = append(m.statusSHAs, sha)
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return status, nil
}

func (m *mockGuardrailsGH) AddPRComment(_ context.Context, _, _ string, number int, body string) (*github.PRComment, error) {
	m.commentBody = append(m.commentBody, body)
	m.commentPRs = append(m.commentPRs, number)
	if m.commentErr != nil {
		return nil, m.commentErr
	}
	return &github.PRComment{Body: body}, nil
}

// stubRegistry plants a fixed set of violations regardless of input, and records
// the disabled-rule list it was handed so a test can assert the passthrough.
type stubRegistry struct {
	out          []architect.Violation
	gotDisabled  []string
	gotChanged   []string
	gotWorktree  string
	evaluateCall int
}

func (s *stubRegistry) Evaluate(_ context.Context, changed []string, worktree string, disabled []string) []architect.Violation {
	s.evaluateCall++
	s.gotChanged = changed
	s.gotWorktree = worktree
	s.gotDisabled = disabled
	return s.out
}

func prFiles(names ...string) []*github.PRFile {
	out := make([]*github.PRFile, 0, len(names))
	for _, n := range names {
		out = append(out, &github.PRFile{Filename: n, Status: "modified"})
	}
	return out
}

func sampleViolations() []architect.Violation {
	return []architect.Violation{
		{Rule: "loc-400", File: "internal/x/big.go", Detail: "file is 450 lines (limit 400)", Risk: pilotapi.RiskMedium},
	}
}

// --- interface conformance --------------------------------------------------

// The real *github.Client must satisfy the narrow gate interface, otherwise the
// mock would be testing a contract production code does not honour.
func TestGuardrailsGate_RealClientSatisfiesInterface(t *testing.T) {
	var _ guardrailsGitHub = github.NewClient("test-github-token")
}

// The real *architect.RuleRegistry must satisfy ruleEvaluator.
func TestGuardrailsGate_RealRegistrySatisfiesEvaluator(t *testing.T) {
	var _ ruleEvaluator = architect.DefaultRuleRegistry("github.com/ylcn91/pilot")
}

// --- disabled / inert -------------------------------------------------------

func TestGuardrailsGate_DisabledIsNoOp(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: false}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 7, "deadbeef")
	if err != nil {
		t.Fatalf("disabled gate must not error: %v", err)
	}
	if v != nil {
		t.Errorf("disabled gate must return no violations, got %+v", v)
	}
	if gh.listCalls != 0 || len(gh.statusCalls) != 0 || len(gh.commentBody) != 0 {
		t.Errorf("disabled gate must make no GitHub calls: list=%d status=%d comment=%d",
			gh.listCalls, len(gh.statusCalls), len(gh.commentBody))
	}
	if reg.evaluateCall != 0 {
		t.Error("disabled gate must not evaluate rules")
	}
}

func TestGuardrailsGate_NilGateInert(t *testing.T) {
	var g *GuardrailsGate
	if g.Enabled() {
		t.Error("nil gate must not be enabled")
	}
}

func TestGuardrailsGate_ZeroConfigInert(t *testing.T) {
	g := NewGuardrailsGate(&mockGuardrailsGH{}, &stubRegistry{}, GuardrailsGateConfig{}, "", "o", "r")
	if g.Enabled() {
		t.Error("zero-value config (Enabled=false) must be inert")
	}
}

// --- report mode (default, fail-open) ---------------------------------------

func TestGuardrailsGate_ReportMode_CleanPRPostsGreenNoComment(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/small.go")}
	reg := &stubRegistry{out: nil} // no violations
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 11, "sha111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 0 {
		t.Errorf("clean PR must have no violations, got %+v", v)
	}
	if len(gh.statusCalls) != 1 {
		t.Fatalf("expected exactly 1 status post, got %d", len(gh.statusCalls))
	}
	if gh.statusCalls[0].State != "success" {
		t.Errorf("clean report-mode status must be success, got %q", gh.statusCalls[0].State)
	}
	if gh.statusCalls[0].Context != guardrailsStatusContext {
		t.Errorf("status context = %q, want %q", gh.statusCalls[0].Context, guardrailsStatusContext)
	}
	if len(gh.commentBody) != 0 {
		t.Errorf("clean PR must not get a comment, got %+v", gh.commentBody)
	}
}

func TestGuardrailsGate_ReportMode_ViolationsStayGreenButComment(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 12, "sha222")
	if err != nil {
		t.Fatalf("report mode must never error: %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("expected the planted violation to be returned, got %+v", v)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "success" {
		t.Fatalf("report mode must keep the status GREEN even with violations, got %+v", gh.statusCalls)
	}
	if !strings.Contains(gh.statusCalls[0].Description, "report-only") {
		t.Errorf("report status description should mark report-only, got %q", gh.statusCalls[0].Description)
	}
	if len(gh.commentBody) != 1 {
		t.Fatalf("expected one PR comment, got %d", len(gh.commentBody))
	}
	if !strings.Contains(gh.commentBody[0], "internal/x/big.go") {
		t.Errorf("comment must name the offending file, got: %s", gh.commentBody[0])
	}
	if !strings.Contains(gh.commentBody[0], guardrailsCommentMarker) {
		t.Error("comment must carry the idempotency marker")
	}
}

func TestGuardrailsGate_EmptyModeDefaultsToReport(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go")}
	reg := &stubRegistry{out: sampleViolations()}
	// Mode left empty but Enabled true => report semantics.
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 13, "sha333"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gh.statusCalls[0].State != "success" {
		t.Errorf("empty mode must behave as report (green), got %q", gh.statusCalls[0].State)
	}
}

// --- block mode (opt-in) ----------------------------------------------------

func TestGuardrailsGate_BlockMode_ViolationsPostFailure(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 21, "sha444")
	if err != nil {
		t.Fatalf("block mode still returns nil error (blocking is via status): %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("expected the violation returned, got %+v", v)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "failure" {
		t.Fatalf("block mode with violations must post a FAILURE status, got %+v", gh.statusCalls)
	}
	if !strings.Contains(gh.statusCalls[0].Description, "blocking") {
		t.Errorf("block status description should say blocking, got %q", gh.statusCalls[0].Description)
	}
	if len(gh.commentBody) != 1 || !strings.Contains(gh.commentBody[0], "block") {
		t.Errorf("block-mode comment should mention block, got %+v", gh.commentBody)
	}
}

func TestGuardrailsGate_BlockMode_CleanPRStaysGreen(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/small.go")}
	reg := &stubRegistry{out: nil}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 22, "sha555"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "success" {
		t.Fatalf("block mode with NO violations must stay green, got %+v", gh.statusCalls)
	}
	if len(gh.commentBody) != 0 {
		t.Errorf("clean PR must not get a comment even in block mode, got %+v", gh.commentBody)
	}
}

// --- fail-open paths --------------------------------------------------------

func TestGuardrailsGate_FailOpen_ListFilesErrorPostsNothing(t *testing.T) {
	gh := &mockGuardrailsGH{filesErr: errors.New("github 500")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 31, "sha666")
	if err != nil {
		t.Fatalf("list error must be swallowed (fail-open), got %v", err)
	}
	if v != nil {
		t.Errorf("list error must yield no violations, got %+v", v)
	}
	if len(gh.statusCalls) != 0 {
		t.Errorf("on a list-files outage the gate must post NO status (must not block on its own failure), got %+v", gh.statusCalls)
	}
	if reg.evaluateCall != 0 {
		t.Error("rules must not run when the file list could not be fetched")
	}
}

func TestGuardrailsGate_EmptyHeadSHASkipped(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 32, "")
	if err != nil {
		t.Fatalf("empty SHA must not error: %v", err)
	}
	if v != nil || gh.listCalls != 0 || len(gh.statusCalls) != 0 {
		t.Errorf("empty head SHA must be a clean skip: v=%+v list=%d status=%d", v, gh.listCalls, len(gh.statusCalls))
	}
}

func TestGuardrailsGate_StatusPostErrorSwallowed(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go"), statusErr: errors.New("status 403")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 33, "sha777")
	if err != nil {
		t.Fatalf("a status-post failure must be swallowed, got %v", err)
	}
	if len(v) != 1 {
		t.Errorf("violations should still be returned despite status error, got %+v", v)
	}
	// The comment is still attempted even when the status post failed.
	if len(gh.commentBody) != 1 {
		t.Errorf("comment should still be posted after a status error, got %d", len(gh.commentBody))
	}
}

func TestGuardrailsGate_CommentPostErrorSwallowed(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/big.go"), commentErr: errors.New("comment 403")}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 34, "sha888"); err != nil {
		t.Fatalf("a comment-post failure must be swallowed, got %v", err)
	}
	if len(gh.statusCalls) != 1 {
		t.Errorf("status should still have been posted, got %d", len(gh.statusCalls))
	}
}

// --- passthrough / wiring ---------------------------------------------------

func TestGuardrailsGate_PassesDisabledRulesAndWorktree(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/a.go", "internal/y/b.go")}
	reg := &stubRegistry{out: nil}
	cfg := GuardrailsGateConfig{Enabled: true, Mode: "report", DisabledRules: []string{"loc-400", "gateway-auth"}}
	g := NewGuardrailsGate(gh, reg, cfg, "/my/worktree", "o", "r")

	if _, err := g.Evaluate(context.Background(), 41, "sha999"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.gotWorktree != "/my/worktree" {
		t.Errorf("registry got worktree %q, want /my/worktree", reg.gotWorktree)
	}
	if strings.Join(reg.gotDisabled, ",") != "loc-400,gateway-auth" {
		t.Errorf("disabled rules not passed through, got %v", reg.gotDisabled)
	}
	if strings.Join(reg.gotChanged, ",") != "internal/x/a.go,internal/y/b.go" {
		t.Errorf("changed files not passed through, got %v", reg.gotChanged)
	}
	if gh.listPRNumber != 41 {
		t.Errorf("list called with PR %d, want 41", gh.listPRNumber)
	}
}

func TestGuardrailsGate_FiltersNilAndEmptyFilenames(t *testing.T) {
	gh := &mockGuardrailsGH{files: []*github.PRFile{
		nil,
		{Filename: ""},
		{Filename: "internal/x/keep.go"},
	}}
	reg := &stubRegistry{out: nil}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 42, "shaaaa"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(reg.gotChanged, ",") != "internal/x/keep.go" {
		t.Errorf("nil/empty filenames must be dropped, got %v", reg.gotChanged)
	}
}

func TestGuardrailsGate_StatusUsesHeadSHA(t *testing.T) {
	gh := &mockGuardrailsGH{files: prFiles("internal/x/small.go")}
	reg := &stubRegistry{out: nil}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 43, "headshaXYZ"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.statusSHAs) != 1 || gh.statusSHAs[0] != "headshaXYZ" {
		t.Errorf("status must be posted against the head SHA, got %v", gh.statusSHAs)
	}
}

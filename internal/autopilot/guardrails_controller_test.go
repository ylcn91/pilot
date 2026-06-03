package autopilot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/testutil"
)

// filesServer returns an httptest server that answers the ListPullRequestFiles
// call handleCIPassed makes on the real client, plus a catch-all 200 "{}".
// The PR is small + has no linked issue so neither the size-floor nor the
// scope-drift escalation fires — isolating the guardrails wiring under test.
func filesServer(t *testing.T, prNumber int) *httptest.Server {
	t.Helper()
	filesPath := fmt.Sprintf("/repos/owner/repo/pulls/%d/files", prNumber)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet && r.URL.Path == filesPath {
			_, _ = w.Write(mustJSON(t, prFiles("internal/x/big.go")))
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
}

// newGateController builds a Controller (EnvDev, RequireApproval=false) wired to
// a guardrails gate over the supplied mock + stub. handleCIPassed talks to the
// real ghClient for ListPullRequestFiles; the gate posts via the mock.
func newGateController(t *testing.T, prNumber int, gate *GuardrailsGate) *Controller {
	t.Helper()
	server := filesServer(t, prNumber)
	t.Cleanup(server.Close)

	ghClient := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	cfg := DefaultConfig()
	cfg.Environment = EnvDev

	c := NewController(cfg, ghClient, nil, "owner", "repo")
	if gate != nil {
		c.SetGuardrailsGate(gate)
	}
	return c
}

// TestHandleCIPassed_InvokesGuardrailsGate_WhenEnabled proves the gate is run as
// a side-effect of handleCIPassed (status + comment posted) AND that, even in
// block mode with violations, the merge path is untouched: the PR still proceeds
// to StageMerging because guardrails NEVER block the merge directly.
func TestHandleCIPassed_InvokesGuardrailsGate_WhenEnabled(t *testing.T) {
	gh := &mockGuardrailsGH{}
	reg := &stubRegistry{out: sampleViolations()}
	gate := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "owner", "repo")

	c := newGateController(t, 90, gate)
	prState := &PRState{PRNumber: 90, PRTitle: "fix(x): small", HeadSHA: "headsha90", Stage: StageCIPassed}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed errored: %v", err)
	}

	if reg.evaluateCall != 1 {
		t.Errorf("guardrails gate must have evaluated exactly once, got %d", reg.evaluateCall)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].Context != guardrailsStatusContext {
		t.Fatalf("gate must post a pilot/guardrails status, got %+v", gh.statusCalls)
	}
	if gh.statusCalls[0].State != "failure" {
		t.Errorf("block mode + violations => failure status, got %q", gh.statusCalls[0].State)
	}
	if len(gh.commentBody) != 1 {
		t.Errorf("gate must post a findings comment, got %d", len(gh.commentBody))
	}
	// Critical: the guardrails failure status must NOT have blocked the merge path.
	if prState.Stage != StageMerging {
		t.Errorf("guardrails must never block the merge: want StageMerging, got %v", prState.Stage)
	}
}

// TestHandleCIPassed_InvokesGate_ReusesFetchedFiles proves the gate consumes the
// files handleCIPassed already fetched rather than issuing its own
// ListPullRequestFiles round-trip (the mock's list counter stays at zero).
func TestHandleCIPassed_InvokesGate_ReusesFetchedFiles(t *testing.T) {
	gh := &mockGuardrailsGH{}
	reg := &stubRegistry{out: nil}
	gate := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "owner", "repo")

	c := newGateController(t, 91, gate)
	prState := &PRState{PRNumber: 91, PRTitle: "fix(x): small", HeadSHA: "headsha91", Stage: StageCIPassed}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed errored: %v", err)
	}
	if gh.listCalls != 0 {
		t.Errorf("gate must reuse handleCIPassed's files, not re-list (got %d list calls)", gh.listCalls)
	}
	if len(reg.gotChanged) != 1 || reg.gotChanged[0] != "internal/x/big.go" {
		t.Errorf("gate must receive the already-fetched changed files, got %v", reg.gotChanged)
	}
}

// TestHandleCIPassed_SkipsGuardrailsGate_WhenDisabled proves a disabled gate is a
// no-op: no status, no comment, no rule evaluation — and the merge proceeds.
func TestHandleCIPassed_SkipsGuardrailsGate_WhenDisabled(t *testing.T) {
	gh := &mockGuardrailsGH{}
	reg := &stubRegistry{out: sampleViolations()}
	gate := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: false}, "/wt", "owner", "repo")

	c := newGateController(t, 92, gate)
	prState := &PRState{PRNumber: 92, PRTitle: "fix(x): small", HeadSHA: "headsha92", Stage: StageCIPassed}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed errored: %v", err)
	}
	if reg.evaluateCall != 0 {
		t.Error("disabled gate must not evaluate rules")
	}
	if len(gh.statusCalls) != 0 || len(gh.commentBody) != 0 {
		t.Errorf("disabled gate must post nothing: status=%d comment=%d", len(gh.statusCalls), len(gh.commentBody))
	}
	if prState.Stage != StageMerging {
		t.Errorf("disabled gate must not affect the merge path, got %v", prState.Stage)
	}
}

// TestHandleCIPassed_NoGate_NoOp proves the common case (no gate wired at all)
// leaves handleCIPassed behaviour exactly as before.
func TestHandleCIPassed_NoGate_NoOp(t *testing.T) {
	c := newGateController(t, 93, nil)
	prState := &PRState{PRNumber: 93, PRTitle: "fix(x): small", HeadSHA: "headsha93", Stage: StageCIPassed}

	if err := c.handleCIPassed(context.Background(), prState); err != nil {
		t.Fatalf("handleCIPassed errored with no gate: %v", err)
	}
	if prState.Stage != StageMerging {
		t.Errorf("no gate must merge as usual, got %v", prState.Stage)
	}
}

package autopilot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
)

// --- comment dedup (update-or-create) ---------------------------------------

func TestGuardrailsGate_Dedup_UpdatesPriorComment(t *testing.T) {
	prior := &github.Comment{ID: 555, Body: guardrailsCommentMarker + "\nold guardrails report"}
	gh := &mockGuardrailsGH{
		files:    prFiles("internal/x/big.go"),
		comments: []*github.Comment{prior},
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 60, "shadedup"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.commentBody) != 0 {
		t.Errorf("dedup must NOT create a new comment, got %d", len(gh.commentBody))
	}
	if len(gh.updatedIDs) != 1 || gh.updatedIDs[0] != 555 {
		t.Fatalf("dedup must update comment 555 in place, got %v", gh.updatedIDs)
	}
	if !strings.Contains(gh.updatedBody[0], "internal/x/big.go") {
		t.Errorf("updated body must contain the new findings, got:\n%s", gh.updatedBody[0])
	}
	if !strings.Contains(gh.updatedBody[0], guardrailsCommentMarker) {
		t.Error("updated body must still carry the marker")
	}
}

func TestGuardrailsGate_Dedup_CreatesWhenNoPriorComment(t *testing.T) {
	// A foreign comment without the marker must NOT be treated as the prior one.
	gh := &mockGuardrailsGH{
		files:    prFiles("internal/x/big.go"),
		comments: []*github.Comment{{ID: 1, Body: "a human said something"}},
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 61, "shacreate"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.updatedIDs) != 0 {
		t.Errorf("no marker => must NOT update a foreign comment, got %v", gh.updatedIDs)
	}
	if len(gh.commentBody) != 1 {
		t.Fatalf("no prior guardrails comment => must create one, got %d", len(gh.commentBody))
	}
}

func TestGuardrailsGate_Dedup_FirstMarkedCommentWins(t *testing.T) {
	gh := &mockGuardrailsGH{
		files: prFiles("internal/x/big.go"),
		comments: []*github.Comment{
			{ID: 10, Body: "human reply"},
			{ID: 20, Body: guardrailsCommentMarker + "\nfirst bot comment"},
			{ID: 30, Body: guardrailsCommentMarker + "\nsecond bot comment"},
		},
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 62, "shafirst"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.updatedIDs) != 1 || gh.updatedIDs[0] != 20 {
		t.Errorf("the first marked comment (20) must be the one updated, got %v", gh.updatedIDs)
	}
}

func TestGuardrailsGate_Dedup_UpdateErrorSwallowed(t *testing.T) {
	prior := &github.Comment{ID: 99, Body: guardrailsCommentMarker}
	gh := &mockGuardrailsGH{
		files:     prFiles("internal/x/big.go"),
		comments:  []*github.Comment{prior},
		updateErr: errors.New("patch 403"),
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "o", "r")

	if _, err := g.Evaluate(context.Background(), 63, "shaupderr"); err != nil {
		t.Fatalf("an update-comment failure must be swallowed (fail-open), got %v", err)
	}
	if len(gh.statusCalls) != 1 {
		t.Errorf("status must still have been posted despite update error, got %d", len(gh.statusCalls))
	}
}

// --- exception workflow -----------------------------------------------------

func TestGuardrailsGate_Exception_FromPRBodySuppressesRule(t *testing.T) {
	gh := &mockGuardrailsGH{
		files:  prFiles("internal/x/big.go"),
		prBody: "This is intentional.\npilot-guardrail-allow: loc-400\n",
	}
	reg := &stubRegistry{out: sampleViolations()} // only loc-400 fires
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 70, "shaexc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("the only rule was waived => no enforced violations, got %+v", v)
	}
	// Waived everything => even block mode posts a SUCCESS status (nothing to block).
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "success" {
		t.Fatalf("a fully-waived PR must stay green even in block mode, got %+v", gh.statusCalls)
	}
	// The exception must still be recorded in a comment, not silently dropped.
	if len(gh.commentBody) != 1 {
		t.Fatalf("an acknowledged exception must produce a comment, got %d", len(gh.commentBody))
	}
	body := gh.commentBody[0]
	if !strings.Contains(body, "Waived") || !strings.Contains(body, "loc-400") {
		t.Errorf("comment must record the waived rule, got:\n%s", body)
	}
}

func TestGuardrailsGate_Exception_FromCommentSuppressesRule(t *testing.T) {
	gh := &mockGuardrailsGH{
		files:    prFiles("internal/x/big.go"),
		comments: []*github.Comment{{ID: 7, Body: "pilot-guardrail-allow: loc-400"}},
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 71, "shaexc2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("a comment directive must waive the rule, got %+v", v)
	}
	if gh.statusCalls[0].State != "success" {
		t.Errorf("waived block-mode PR must stay green, got %q", gh.statusCalls[0].State)
	}
}

func TestGuardrailsGate_Exception_PartialWaiveStillBlocks(t *testing.T) {
	gh := &mockGuardrailsGH{
		files:  prFiles("internal/x/big.go", "internal/executor/bad.go"),
		prBody: "pilot-guardrail-allow: loc-400",
	}
	reg := &stubRegistry{out: append(sampleViolations(),
		violation("forbidden-import", "internal/executor/bad.go"))}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 72, "shapartial")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 1 || v[0].Rule != "forbidden-import" {
		t.Fatalf("only loc-400 waived; forbidden-import must still be enforced, got %+v", v)
	}
	if len(gh.statusCalls) != 1 || gh.statusCalls[0].State != "failure" {
		t.Fatalf("an unwaived violation in block mode must still post failure, got %+v", gh.statusCalls)
	}
	body := gh.commentBody[0]
	if !strings.Contains(body, "forbidden-import") {
		t.Error("comment must list the enforced (unwaived) violation")
	}
	if !strings.Contains(body, "Waived") || !strings.Contains(body, "loc-400") {
		t.Error("comment must also record the waived loc-400 exception")
	}
}

func TestGuardrailsGate_Exception_UnusedAllowNotReported(t *testing.T) {
	// Allowing a rule that did not fire must not fabricate a "Waived" section.
	gh := &mockGuardrailsGH{
		files:  prFiles("internal/x/big.go"),
		prBody: "pilot-guardrail-allow: gateway-auth",
	}
	reg := &stubRegistry{out: sampleViolations()} // loc-400 fires, gateway-auth does not
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 73, "shaunused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("the firing rule must stay enforced, got %+v", v)
	}
	if strings.Contains(gh.commentBody[0], "Waived") {
		t.Errorf("a directive for a non-firing rule must not produce a Waived section, got:\n%s", gh.commentBody[0])
	}
}

// --- scanPR fail-open -------------------------------------------------------

func TestGuardrailsGate_ScanPR_BodyFetchErrorIsFailOpen(t *testing.T) {
	// GetPullRequest failing must not abort: the rule still enforces (no
	// exception is honoured), and the gate still posts.
	gh := &mockGuardrailsGH{
		files: prFiles("internal/x/big.go"),
		prErr: errors.New("pr 500"),
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 80, "shabodyfail")
	if err != nil {
		t.Fatalf("PR body fetch error must be swallowed, got %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("no exception honoured => violation enforced, got %+v", v)
	}
	if gh.statusCalls[0].State != "failure" {
		t.Errorf("block-mode enforced violation must still post failure, got %q", gh.statusCalls[0].State)
	}
}

func TestGuardrailsGate_ScanPR_CommentsFetchErrorIsFailOpen(t *testing.T) {
	// ListIssueComments failing means: no comment-based exceptions, no dedup
	// lookup => the gate must CREATE a fresh comment (cannot find a prior one).
	gh := &mockGuardrailsGH{
		files:      prFiles("internal/x/big.go"),
		prBody:     "pilot-guardrail-allow: loc-400",
		listCmtErr: errors.New("comments 500"),
	}
	reg := &stubRegistry{out: sampleViolations()}
	g := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "block"}, "/wt", "o", "r")

	v, err := g.Evaluate(context.Background(), 81, "shacmtfail")
	if err != nil {
		t.Fatalf("comments fetch error must be swallowed, got %v", err)
	}
	// The PR-body exception still applies (body was fetched fine), so loc-400 is waived.
	if len(v) != 0 {
		t.Fatalf("body exception must still waive loc-400 despite comment fetch error, got %+v", v)
	}
	// Dedup could not run => a new comment is created, never an update.
	if len(gh.updatedIDs) != 0 {
		t.Errorf("a failed comment list must not lead to an update, got %v", gh.updatedIDs)
	}
	if len(gh.commentBody) != 1 {
		t.Errorf("the exception acknowledgement must still be created, got %d", len(gh.commentBody))
	}
}

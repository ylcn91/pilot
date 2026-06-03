package autopilot

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/architect"
)

// panickingRegistry is a ruleEvaluator that always panics, used to drive the
// recover() guard in runGuardrailsGate. The panic originates deep inside
// GuardrailsGate.EvaluateWithFiles (at registry.Evaluate), proving the
// controller's deferred recover catches a panic from anywhere in the gate, not
// just a shallow one.
type panickingRegistry struct {
	called bool
}

func (p *panickingRegistry) Evaluate(_ context.Context, _ []string, _ string, _ []string) []architect.Violation {
	p.called = true
	panic("simulated guardrails rule evaluation crash")
}

// TestRunGuardrailsGate_RecoversPanic asserts the fail-open contract: a gate
// whose rule evaluation panics must NOT crash the autopilot tick. runGuardrailsGate
// recovers the panic, logs a WARN, and returns normally so the surrounding
// handleCIPassed proceeds untouched.
func TestRunGuardrailsGate_RecoversPanic(t *testing.T) {
	gh := &mockGuardrailsGH{}
	reg := &panickingRegistry{}
	// repoPath "/wt" is not a git work tree, so materializeHead passes the path
	// straight through and the gate reaches registry.Evaluate, which panics.
	gate := NewGuardrailsGate(gh, reg, GuardrailsGateConfig{Enabled: true, Mode: "report"}, "/wt", "owner", "repo")

	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	c := NewController(cfg, nil, nil, "owner", "repo")
	c.SetGuardrailsGate(gate)

	var buf bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	prState := &PRState{PRNumber: 77, HeadSHA: "panicsha", Stage: StageCIPassed}

	// Must not panic out of the call: the deferred recover() swallows it.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("runGuardrailsGate must recover the gate panic, but it propagated: %v", r)
		}
	}()

	c.runGuardrailsGate(context.Background(), prState, prFiles("internal/x/big.go"))

	if !reg.called {
		t.Fatal("precondition: panicking evaluator was never reached — adjust the seam")
	}
	logged := buf.String()
	if !strings.Contains(logged, "guardrails gate panicked") {
		t.Errorf("expected a fail-open panic WARN, got: %q", logged)
	}
	// The panic value should be surfaced in the log for diagnosis.
	if !strings.Contains(logged, "simulated guardrails rule evaluation crash") {
		t.Errorf("panic value must be logged for diagnosis, got: %q", logged)
	}
}

// TestRunGuardrailsGate_NilGate_NoOp guards the cheap early-return: a nil gate
// must be a no-op and never panic when runGuardrailsGate is invoked directly.
func TestRunGuardrailsGate_NilGate_NoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Environment = EnvDev
	c := NewController(cfg, nil, nil, "owner", "repo")
	// No gate wired.

	prState := &PRState{PRNumber: 78, HeadSHA: "sha78", Stage: StageCIPassed}
	c.runGuardrailsGate(context.Background(), prState, nil) // must not panic
}

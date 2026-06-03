package architect

import (
	"context"
	"errors"
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/pilotapi"
)

// TestPropose_OfflineSkipsBackend proves the offline analyzer never invokes the
// backend yet still returns real, signal-derived findings. This is the contract
// the dry-run CLI relies on: graph-derived output with zero network/subprocess.
func TestPropose_OfflineSkipsBackend(t *testing.T) {
	mb := &mockBackend{output: "[]", err: errors.New("backend must not be called")}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(true))
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}

	got, err := a.Propose(context.Background(), sampleSignals())
	if err != nil {
		t.Fatalf("offline Propose must not error: %v", err)
	}
	if mb.calls != 0 {
		t.Fatalf("offline Propose must not call the backend, got %d calls", mb.calls)
	}
	if len(got) == 0 {
		t.Fatal("offline Propose over non-empty signals must produce findings")
	}
}

// TestPropose_OfflineEmptySignals proves the empty-signal short-circuit still
// holds in offline mode (no findings, no backend, no error).
func TestPropose_OfflineEmptySignals(t *testing.T) {
	mb := &mockBackend{}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(true))
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	got, err := a.Propose(context.Background(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty signals => 0 findings, got %d", len(got))
	}
	if mb.calls != 0 {
		t.Fatalf("empty offline Propose must not call backend, got %d", mb.calls)
	}
}

// TestPropose_OfflineMatchesSynthesizer proves the offline Propose path is exactly
// SynthesizeFindings — same titles in the same order — so the two stay coupled.
func TestPropose_OfflineMatchesSynthesizer(t *testing.T) {
	signals := []Signal{
		{Kind: "import_cycle_risk", File: "pkg/a", Risk: pilotapi.RiskHigh},
		{Kind: "unused_dep", File: "mod/x", Risk: pilotapi.RiskLow},
	}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(true))

	got, err := a.Propose(context.Background(), signals)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	want := SynthesizeFindings(signals)
	if len(got) != len(want) {
		t.Fatalf("offline Propose len %d != synthesizer len %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Title != want[i].Title {
			t.Errorf("finding %d: Propose title %q != synthesizer %q", i, got[i].Title, want[i].Title)
		}
	}
}

// TestPropose_OnlineStillCallsBackend proves WithOffline(false) (the default)
// preserves the original backend-driven behaviour: the analyzer invokes the
// backend exactly once.
func TestPropose_OnlineStillCallsBackend(t *testing.T) {
	mb := &mockBackend{output: `[{"title":"x","kind":"refactor","risk":"low"}]`}
	a := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(false))
	a.newBackend = func(*executor.StageConfig, executor.BackendConfig) (executor.Backend, error) {
		return mb, nil
	}
	if _, err := a.Propose(context.Background(), sampleSignals()); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if mb.calls != 1 {
		t.Fatalf("online Propose must call backend once, got %d", mb.calls)
	}
}

// TestWithOffline_Toggles proves the option sets the flag both ways.
func TestWithOffline_Toggles(t *testing.T) {
	on := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(true))
	if !on.offline {
		t.Error("WithOffline(true) must set offline")
	}
	off := NewAnalyzer(nil, executor.BackendConfig{}, "", WithOffline(false))
	if off.offline {
		t.Error("WithOffline(false) must leave offline unset")
	}
	def := NewAnalyzer(nil, executor.BackendConfig{}, "")
	if def.offline {
		t.Error("default analyzer must not be offline")
	}
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/config"
)

func TestArchitectEnabled(t *testing.T) {
	tests := []struct {
		name string
		arch *config.ArchitectConfig
		want bool
	}{
		{"nil block", nil, false},
		{"disabled", &config.ArchitectConfig{Enabled: false, Schedule: "0 9 * * *"}, false},
		{"enabled no schedule", &config.ArchitectConfig{Enabled: true, Schedule: ""}, false},
		{"enabled with schedule", &config.ArchitectConfig{Enabled: true, Schedule: "0 9 * * *"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Architect: tt.arch}
			if got := architectEnabled(cfg); got != tt.want {
				t.Fatalf("architectEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArchitectRadarConfig(t *testing.T) {
	cfg := &config.Config{
		Architect: &config.ArchitectConfig{
			Enabled:    true,
			Schedule:   "0 9 * * *",
			Thresholds: config.ArchitectThresholds{MinCoverage: 72},
			Signals:    []string{"loc", "todo"},
		},
	}
	rc := architectRadarConfig(cfg, "/tmp/proj")
	if rc.ProjectPath != "/tmp/proj" {
		t.Fatalf("ProjectPath = %q, want /tmp/proj", rc.ProjectPath)
	}
	if rc.Options.MinCoverage != 72 {
		t.Fatalf("MinCoverage = %v, want 72", rc.Options.MinCoverage)
	}
	if len(rc.Options.Signals) != 2 || rc.Options.Signals[0] != "loc" {
		t.Fatalf("Signals = %v, want [loc todo]", rc.Options.Signals)
	}
}

func TestArchitectRadarConfig_NilBlock(t *testing.T) {
	rc := architectRadarConfig(&config.Config{}, "/p")
	if rc.ProjectPath != "/p" {
		t.Fatalf("ProjectPath = %q, want /p", rc.ProjectPath)
	}
	if rc.Options.MinCoverage != 0 || rc.Options.Signals != nil {
		t.Fatalf("nil architect block must yield zero options, got %+v", rc.Options)
	}
}

func TestArchitectSchedulerConfig(t *testing.T) {
	cfg := &config.Config{
		Architect: &config.ArchitectConfig{
			Enabled:  true,
			Schedule: "*/5 * * * *",
			Timezone: "America/New_York",
		},
	}
	sc := architectSchedulerConfig(cfg)
	if !sc.Enabled || sc.Schedule != "*/5 * * * *" || sc.Timezone != "America/New_York" {
		t.Fatalf("unexpected scheduler config: %+v", sc)
	}
}

func TestArchitectSchedulerConfig_NilBlock(t *testing.T) {
	sc := architectSchedulerConfig(&config.Config{})
	if sc.Enabled || sc.Schedule != "" || sc.Timezone != "" {
		t.Fatalf("nil architect block must yield zero scheduler config, got %+v", sc)
	}
}

// TestStartArchitectScheduler_Wires proves the daemon wiring constructs a live
// scheduler that refreshes the gateway-shared store. It mirrors setupGateway's
// store construction (which only happens when architectEnabled) by seeding
// p.architectStore directly, then runs the wiring function in isolation.
func TestStartArchitectScheduler_Wires(t *testing.T) {
	root := t.TempDir()
	writeOversizedGoFixture(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := architect.NewFindingsStore()
	p := &pollingRuntime{
		ctx:            ctx,
		projectPath:    root,
		architectStore: store,
		cfg: &config.Config{
			Architect: &config.ArchitectConfig{
				Enabled:  true,
				Schedule: "0 9 * * *",
				Timezone: "UTC",
			},
		},
	}

	p.startArchitectScheduler()

	if p.architectScheduler == nil {
		t.Fatal("startArchitectScheduler must record a scheduler on the runtime")
	}
	defer p.architectScheduler.Stop()

	if !p.architectScheduler.IsRunning() {
		t.Fatal("scheduler must be running after wiring")
	}

	// The boot catch-up scan runs in a goroutine; the shared store the gateway
	// also reads must fill within a short window.
	waitForStore(t, store, 1)
}

func TestStartArchitectScheduler_NoStoreIsNoop(t *testing.T) {
	p := &pollingRuntime{
		ctx:         context.Background(),
		projectPath: t.TempDir(),
		cfg: &config.Config{
			Architect: &config.ArchitectConfig{Enabled: true, Schedule: "0 9 * * *"},
		},
	}
	// architectStore is nil (feature disabled at gateway, or no gateway), so
	// wiring must short-circuit.
	p.startArchitectScheduler()
	if p.architectScheduler != nil {
		t.Fatal("wiring must be a no-op when the findings store is absent")
	}
}

func TestStartArchitectScheduler_DisabledIsNoop(t *testing.T) {
	p := &pollingRuntime{
		ctx:            context.Background(),
		projectPath:    t.TempDir(),
		architectStore: architect.NewFindingsStore(),
		cfg: &config.Config{
			Architect: &config.ArchitectConfig{Enabled: false, Schedule: "0 9 * * *"},
		},
	}
	p.startArchitectScheduler()
	if p.architectScheduler != nil {
		t.Fatal("wiring must be a no-op when architect is disabled")
	}
}

// writeOversizedGoFixture writes a Go file over the LOC threshold so the radar's
// deterministic LOC collector yields a finding.
func writeOversizedGoFixture(t *testing.T, root string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 0; i < 420; i++ {
		b.WriteString("// padding line to push the file over the LOC threshold\n")
	}
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// waitForStore polls the gateway-shared store until it holds at least want
// findings or the deadline elapses (the catch-up scan runs asynchronously).
func waitForStore(t *testing.T, store *architect.FindingsStore, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if store.Len() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("store did not reach %d findings (have %d)", want, store.Len())
}

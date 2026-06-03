package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/testutil"
)

// newGuardrailsTestController builds a bare autopilot controller with a fake
// GitHub client and no approval manager. It never makes a network call: the
// guardrails gate is only constructed and wired here, never evaluated.
func newGuardrailsTestController(t *testing.T) *autopilot.Controller {
	t.Helper()
	gh := github.NewClient(testutil.FakeGitHubToken)
	return autopilot.NewController(autopilot.DefaultConfig(), gh, nil, "owner", "repo")
}

// writeGoMod creates a temp project dir containing a go.mod with the given
// module path and returns the dir. modulePrefixFromGoMod must read it back.
func writeGoMod(t *testing.T, modulePath string) string {
	t.Helper()
	dir := t.TempDir()
	body := "module " + modulePath + "\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

func TestMaybeAttachGuardrails_EnabledAttachesReportGate(t *testing.T) {
	controller := newGuardrailsTestController(t)
	projectPath := writeGoMod(t, "github.com/ylcn91/pilot")
	cfg := &config.Config{Guardrails: &config.GuardrailsConfig{Enabled: true}}

	maybeAttachGuardrails(controller, cfg, github.NewClient(testutil.FakeGitHubToken), "owner", "repo", projectPath)

	gate := controller.GuardrailsGate()
	if gate == nil {
		t.Fatal("expected a non-nil guardrails gate when enabled")
	}
	if !gate.Enabled() {
		t.Error("expected gate.Enabled() == true")
	}
	if got, want := gate.Mode(), config.GuardrailsModeReport; got != want {
		t.Errorf("default mode = %q, want %q (report)", got, want)
	}
}

func TestMaybeAttachGuardrails_BlockModePropagates(t *testing.T) {
	controller := newGuardrailsTestController(t)
	projectPath := writeGoMod(t, "github.com/ylcn91/pilot")
	cfg := &config.Config{Guardrails: &config.GuardrailsConfig{
		Enabled:       true,
		Mode:          config.GuardrailsModeBlock,
		DisabledRules: []string{"loc-400"},
	}}

	maybeAttachGuardrails(controller, cfg, github.NewClient(testutil.FakeGitHubToken), "owner", "repo", projectPath)

	gate := controller.GuardrailsGate()
	if gate == nil {
		t.Fatal("expected a non-nil guardrails gate when enabled")
	}
	if got, want := gate.Mode(), config.GuardrailsModeBlock; got != want {
		t.Errorf("mode = %q, want %q (block)", got, want)
	}
}

func TestMaybeAttachGuardrails_DisabledStaysDormant(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"nil guardrails", &config.Config{Guardrails: nil}},
		{"disabled guardrails", &config.Config{Guardrails: &config.GuardrailsConfig{Enabled: false}}},
		{"disabled block-mode", &config.Config{Guardrails: &config.GuardrailsConfig{Enabled: false, Mode: config.GuardrailsModeBlock}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := newGuardrailsTestController(t)
			projectPath := writeGoMod(t, "github.com/ylcn91/pilot")

			maybeAttachGuardrails(controller, tc.cfg, github.NewClient(testutil.FakeGitHubToken), "owner", "repo", projectPath)

			if gate := controller.GuardrailsGate(); gate != nil {
				t.Errorf("expected nil (dormant) gate, got %#v", gate)
			}
		})
	}
}

func TestModulePrefixFromGoMod(t *testing.T) {
	t.Run("reads module path", func(t *testing.T) {
		dir := writeGoMod(t, "github.com/acme/widget")
		if got, want := modulePrefixFromGoMod(dir), "github.com/acme/widget"; got != want {
			t.Errorf("modulePrefixFromGoMod = %q, want %q", got, want)
		}
	})
	t.Run("missing go.mod yields empty", func(t *testing.T) {
		if got := modulePrefixFromGoMod(t.TempDir()); got != "" {
			t.Errorf("modulePrefixFromGoMod = %q, want empty", got)
		}
	})
}

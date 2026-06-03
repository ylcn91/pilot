package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/architect"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/config"
)

// maybeAttachGuardrails wires the architectural guardrails gate into an
// autopilot controller when (and only when) the feature is enabled in config.
//
// It is the composition-root seam that activates the otherwise-dormant
// Controller.runGuardrailsGate path: the gate is fully built in
// internal/autopilot but stays inert until a *GuardrailsGate is injected here.
// cmd/pilot is the only place allowed to import both internal/architect (for the
// rule registry) and internal/autopilot (for the gate), so the rule registry is
// passed through the autopilot.ruleEvaluator interface and no import cycle forms.
//
// When cfg.Guardrails is nil or disabled this is a no-op: the controller's
// guardrailsGate stays nil and existing behaviour is unchanged (fully fail-open).
func maybeAttachGuardrails(controller *autopilot.Controller, cfg *config.Config, ghClient *github.Client, owner, repo, projectPath string) {
	if controller == nil || cfg == nil || cfg.Guardrails == nil || !cfg.Guardrails.Enabled {
		return
	}

	registry := architect.DefaultRuleRegistry(modulePrefixFromGoMod(projectPath))
	gateCfg := autopilot.GuardrailsGateConfig{
		Enabled:       true,
		Mode:          cfg.Guardrails.EffectiveMode(),
		DisabledRules: cfg.Guardrails.DisabledRules,
	}
	gate := autopilot.NewGuardrailsGate(ghClient, registry, gateCfg, projectPath, owner, repo)
	controller.SetGuardrailsGate(gate)
}

// modulePrefixFromGoMod reads go.mod under dir and returns the declared module
// path, or "" when go.mod is missing or has no module directive. The empty
// prefix degrades the forbidden-import rule to no internal-scoped findings,
// keeping the gate fail-open. Best-effort by design: a guardrails wiring path
// must never fail process startup over an unreadable go.mod.
func modulePrefixFromGoMod(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			if path := strings.TrimSpace(rest); path != "" {
				return path
			}
		}
	}
	return ""
}

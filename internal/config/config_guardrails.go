package config

import "fmt"

// Guardrails mode constants. Mode selects what a guardrail violation does on a
// pull request:
//
//   - GuardrailsModeReport (the default): violations are surfaced as a commit
//     status + PR comment but never fail the check or block the PR. This is the
//     safe, fail-open default — turning guardrails on can only ever add
//     information, never break an existing autopilot flow.
//   - GuardrailsModeBlock: violations additionally mark the commit status as a
//     failure so branch protection / autopilot gating can hold the PR.
const (
	GuardrailsModeReport = "report"
	GuardrailsModeBlock  = "block"
)

// GuardrailsConfig configures the per-PR architectural guardrails: repo-specific
// rules (file LOC ceiling, forbidden import edges, gateway auth) evaluated
// against a PR's changed files and surfaced as a commit status + PR comment.
//
// The zero value is inert: Enabled defaults to false, so guardrails do nothing
// until explicitly turned on. When enabled but Mode is left empty, Mode is
// treated as "report" at use-site, keeping the feature fail-open by default —
// it reports violations without ever blocking a PR. Mode "block" is strictly
// opt-in.
type GuardrailsConfig struct {
	// Enabled gates the entire guardrails feature. False/absent => inert.
	Enabled bool `yaml:"enabled,omitempty"`

	// Mode selects report-only ("report", the default) vs PR-blocking
	// ("block") behaviour. Empty is treated as "report" at use-site.
	Mode string `yaml:"mode,omitempty"`

	// DisabledRules lists rule names (Rule.Name) that must not run even when
	// the feature is enabled, letting a repo opt out of a single noisy rule
	// without disabling guardrails wholesale. Unknown names are ignored.
	DisabledRules []string `yaml:"disabled_rules,omitempty"`
}

// EffectiveMode returns the mode to apply, substituting the report-only default
// when Mode is empty. It does not validate; call Validate first to reject an
// unknown mode.
func (g *GuardrailsConfig) EffectiveMode() string {
	if g == nil || g.Mode == "" {
		return GuardrailsModeReport
	}
	return g.Mode
}

// Blocking reports whether a violation should fail the commit status (and thus
// be able to hold a PR). Only an enabled config in "block" mode blocks; every
// other combination is report-only, preserving the fail-open default.
func (g *GuardrailsConfig) Blocking() bool {
	return g != nil && g.Enabled && g.Mode == GuardrailsModeBlock
}

// IsRuleDisabled reports whether the rule with the given name has been opted
// out via DisabledRules.
func (g *GuardrailsConfig) IsRuleDisabled(name string) bool {
	if g == nil {
		return false
	}
	for _, r := range g.DisabledRules {
		if r == name {
			return true
		}
	}
	return false
}

// Validate checks an enabled GuardrailsConfig: Mode (when set) must be one of
// "report" or "block". An empty Mode is accepted and means "report". A nil or
// disabled config is always valid — guardrails are opt-in and fail-open, so a
// misconfigured-but-disabled block can never break config loading.
func (g *GuardrailsConfig) Validate() error {
	if g == nil || !g.Enabled {
		return nil
	}
	switch g.Mode {
	case "", GuardrailsModeReport, GuardrailsModeBlock:
		return nil
	default:
		return fmt.Errorf("guardrails.mode must be %q or %q, got %q",
			GuardrailsModeReport, GuardrailsModeBlock, g.Mode)
	}
}

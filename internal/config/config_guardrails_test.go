package config

import (
	"strings"
	"testing"
)

func TestGuardrailsConfig_NilIsValid(t *testing.T) {
	var g *GuardrailsConfig
	if err := g.Validate(); err != nil {
		t.Fatalf("nil guardrails must be valid: %v", err)
	}
	c := baseValidConfig()
	c.Guardrails = nil
	if err := c.Validate(); err != nil {
		t.Fatalf("nil guardrails on config must be valid: %v", err)
	}
}

func TestGuardrailsConfig_DisabledSkipsModeCheck(t *testing.T) {
	g := &GuardrailsConfig{Enabled: false, Mode: "garbage"}
	if err := g.Validate(); err != nil {
		t.Fatalf("disabled guardrails must skip mode validation, got %v", err)
	}
}

func TestGuardrailsConfig_ValidateModes(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"empty defaults to report", "", false},
		{"report accepted", GuardrailsModeReport, false},
		{"block accepted", GuardrailsModeBlock, false},
		{"unknown rejected", "warn", true},
		{"uppercase rejected", "REPORT", true},
		{"trailing space rejected", "report ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &GuardrailsConfig{Enabled: true, Mode: tc.mode}
			err := g.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("mode %q: expected error, got nil", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("mode %q: unexpected error: %v", tc.mode, err)
			}
		})
	}
}

func TestGuardrailsConfig_ValidateErrorMentionsModes(t *testing.T) {
	g := &GuardrailsConfig{Enabled: true, Mode: "nope"}
	err := g.Validate()
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "report") || !strings.Contains(err.Error(), "block") {
		t.Errorf("error should name valid modes, got: %v", err)
	}
}

func TestGuardrailsConfig_EffectiveMode(t *testing.T) {
	cases := []struct {
		name string
		g    *GuardrailsConfig
		want string
	}{
		{"nil", nil, GuardrailsModeReport},
		{"empty mode", &GuardrailsConfig{Enabled: true}, GuardrailsModeReport},
		{"explicit report", &GuardrailsConfig{Enabled: true, Mode: GuardrailsModeReport}, GuardrailsModeReport},
		{"explicit block", &GuardrailsConfig{Enabled: true, Mode: GuardrailsModeBlock}, GuardrailsModeBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.EffectiveMode(); got != tc.want {
				t.Errorf("EffectiveMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGuardrailsConfig_Blocking(t *testing.T) {
	cases := []struct {
		name string
		g    *GuardrailsConfig
		want bool
	}{
		{"nil never blocks", nil, false},
		{"disabled block-mode does not block", &GuardrailsConfig{Enabled: false, Mode: GuardrailsModeBlock}, false},
		{"enabled report does not block", &GuardrailsConfig{Enabled: true, Mode: GuardrailsModeReport}, false},
		{"enabled empty mode does not block", &GuardrailsConfig{Enabled: true}, false},
		{"enabled block blocks", &GuardrailsConfig{Enabled: true, Mode: GuardrailsModeBlock}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.Blocking(); got != tc.want {
				t.Errorf("Blocking() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGuardrailsConfig_IsRuleDisabled(t *testing.T) {
	g := &GuardrailsConfig{Enabled: true, DisabledRules: []string{"loc-400", "gateway-auth"}}
	if !g.IsRuleDisabled("loc-400") {
		t.Error("loc-400 should be disabled")
	}
	if !g.IsRuleDisabled("gateway-auth") {
		t.Error("gateway-auth should be disabled")
	}
	if g.IsRuleDisabled("forbidden-import") {
		t.Error("forbidden-import should not be disabled")
	}
	var nilG *GuardrailsConfig
	if nilG.IsRuleDisabled("loc-400") {
		t.Error("nil config disables nothing")
	}
}

func TestGuardrailsConfig_DefaultsAreDisabledAndReport(t *testing.T) {
	// A freshly-declared (zero-value) config must be inert and fail-open.
	g := &GuardrailsConfig{}
	if g.Enabled {
		t.Error("zero-value guardrails must default to disabled")
	}
	if g.EffectiveMode() != GuardrailsModeReport {
		t.Errorf("zero-value mode must resolve to report, got %q", g.EffectiveMode())
	}
	if g.Blocking() {
		t.Error("zero-value guardrails must not block")
	}
	if err := g.Validate(); err != nil {
		t.Errorf("zero-value guardrails must validate: %v", err)
	}
}

func TestConfig_ValidateWiresGuardrails(t *testing.T) {
	c := baseValidConfig()
	c.Guardrails = &GuardrailsConfig{Enabled: true, Mode: "invalid-mode"}
	if err := c.Validate(); err == nil {
		t.Fatal("Config.Validate must reject an invalid guardrails mode")
	}

	c.Guardrails = &GuardrailsConfig{Enabled: true, Mode: GuardrailsModeBlock}
	if err := c.Validate(); err != nil {
		t.Fatalf("Config.Validate must accept a valid guardrails config: %v", err)
	}
}

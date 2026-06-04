package main

import (
	"testing"

	"github.com/ylcn91/pilot/internal/autopilot"
)

func TestResolveAutopilotEnvPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		defaultEnv string
		flagEnv    string
		wantEnv    string
		wantEnable bool
		wantErr    bool
	}{
		{
			name:       "flag wins over config default",
			defaultEnv: "stage",
			flagEnv:    "prod",
			wantEnv:    "prod",
			wantEnable: true,
		},
		{
			name:       "config default used when flag empty",
			defaultEnv: "prod",
			flagEnv:    "",
			wantEnv:    "prod",
			wantEnable: true,
		},
		{
			name:       "no flag and no default is a no-op",
			defaultEnv: "",
			flagEnv:    "",
			wantEnv:    "stage", // built-in default from DefaultConfig
			wantEnable: false,
		},
		{
			name:       "unknown flag env errors",
			defaultEnv: "",
			flagEnv:    "nope",
			wantErr:    true,
		},
		{
			name:       "unknown config default errors",
			defaultEnv: "bogus",
			flagEnv:    "",
			wantErr:    true,
		},
		{
			name:       "flag wins even when config default invalid",
			defaultEnv: "bogus",
			flagEnv:    "dev",
			wantEnv:    "dev",
			wantEnable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := autopilot.DefaultConfig()
			cfg.DefaultEnvironment = tt.defaultEnv

			_, err := resolveAutopilotEnv(cfg, tt.flagEnv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveAutopilotEnv() err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cfg.Enabled != tt.wantEnable {
				t.Errorf("Enabled = %v, want %v", cfg.Enabled, tt.wantEnable)
			}
			if got := cfg.EnvironmentName(); got != tt.wantEnv {
				t.Errorf("EnvironmentName() = %q, want %q", got, tt.wantEnv)
			}
		})
	}
}

func TestResolveAutopilotEnvEnablesAutopilot(t *testing.T) {
	cfg := autopilot.DefaultConfig()
	if cfg.Enabled {
		t.Fatal("expected default autopilot to be disabled")
	}
	cfg.DefaultEnvironment = "dev"

	if _, err := resolveAutopilotEnv(cfg, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected autopilot enabled when default_environment selects an env")
	}
}

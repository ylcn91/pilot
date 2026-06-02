package config

import (
	"testing"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name:    "ValidDefaultConfig",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "NilGateway",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway = nil
				return c
			}(),
			wantErr:     true,
			errContains: "gateway configuration is required",
		},
		{
			name: "InvalidPortZero",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway.Port = 0
				return c
			}(),
			wantErr:     true,
			errContains: "invalid gateway port",
		},
		{
			name: "InvalidPortNegative",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway.Port = -1
				return c
			}(),
			wantErr:     true,
			errContains: "invalid gateway port",
		},
		{
			name: "InvalidPortTooHigh",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway.Port = 65536
				return c
			}(),
			wantErr:     true,
			errContains: "invalid gateway port",
		},
		{
			name: "ValidPortMinimum",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway.Port = 1
				return c
			}(),
			wantErr: false,
		},
		{
			name: "ValidPortMaximum",
			config: func() *Config {
				c := DefaultConfig()
				c.Gateway.Port = 65535
				return c
			}(),
			wantErr: false,
		},
		{
			name: "APITokenAuthWithoutToken",
			config: func() *Config {
				c := DefaultConfig()
				c.Auth = &gateway.AuthConfig{
					Type:  gateway.AuthTypeAPIToken,
					Token: "",
				}
				return c
			}(),
			wantErr:     true,
			errContains: "API token is required",
		},
		{
			name: "APITokenAuthWithToken",
			config: func() *Config {
				c := DefaultConfig()
				c.Auth = &gateway.AuthConfig{
					Type:  gateway.AuthTypeAPIToken,
					Token: "valid-token",
				}
				return c
			}(),
			wantErr: false,
		},
		{
			name: "ClaudeCodeAuthWithoutToken",
			config: func() *Config {
				c := DefaultConfig()
				c.Auth = &gateway.AuthConfig{
					Type: gateway.AuthTypeClaudeCode,
				}
				return c
			}(),
			wantErr: false, // ClaudeCode auth doesn't require a token
		},
		{
			name: "NilAuth",
			config: func() *Config {
				c := DefaultConfig()
				c.Auth = nil
				return c
			}(),
			wantErr: false, // Nil auth is allowed
		},
		{
			name: "NilPipelinePasses",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.Pipeline = nil
				return c
			}(),
			wantErr: false,
		},
		{
			name: "ValidThreeStagePipeline",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.Pipeline = &executor.PipelineConfig{
					Plan:    &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
					Execute: &executor.StageConfig{Type: executor.BackendTypeCodexExec},
					Review:  &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
				}
				return c
			}(),
			wantErr: false,
		},
		{
			name: "PipelineRejectsCodexAppServer",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.Pipeline = &executor.PipelineConfig{
					Execute: &executor.StageConfig{Type: "codex-app-server"},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "is not a runnable Backend",
		},
		{
			name: "PipelineRejectsUnknownType",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.Pipeline = &executor.PipelineConfig{
					Plan: &executor.StageConfig{Type: "bogus-backend"},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "is not a known backend",
		},
		{
			name: "PipelineRejectsEmptyStageType",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.Pipeline = &executor.PipelineConfig{
					Review: &executor.StageConfig{},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "type is required",
		},
		{
			name: "NilTDDPasses",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = nil
				return c
			}(),
			wantErr: false,
		},
		{
			name: "DisabledTDDPasses",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = &executor.TDDConfig{
					Enabled:    false,
					TestAuthor: &executor.StageConfig{Type: "codex-app-server"},
				}
				return c
			}(),
			wantErr: false,
		},
		{
			name: "ValidFourRoleTDD",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = &executor.TDDConfig{
					Enabled:     true,
					Architect:   &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
					TestAuthor:  &executor.StageConfig{Type: executor.BackendTypeCodexExec},
					Implementer: &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
					QA:          &executor.StageConfig{Type: executor.BackendTypeClaudeCode},
				}
				return c
			}(),
			wantErr: false,
		},
		{
			name: "TDDRejectsCodexAppServer",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = &executor.TDDConfig{
					Enabled:    true,
					TestAuthor: &executor.StageConfig{Type: "codex-app-server"},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "is not a runnable Backend",
		},
		{
			name: "TDDRejectsUnknownType",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = &executor.TDDConfig{
					Enabled:   true,
					Architect: &executor.StageConfig{Type: "bogus-backend"},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "is not a known backend",
		},
		{
			name: "TDDRejectsEmptyRoleType",
			config: func() *Config {
				c := DefaultConfig()
				c.Executor.TDD = &executor.TDDConfig{
					Enabled:     true,
					Implementer: &executor.StageConfig{},
				}
				return c
			}(),
			wantErr:     true,
			errContains: "type is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() should return error")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

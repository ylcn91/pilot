package config

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/quality"
)

// GH-1124: Test bounds and orchestrator validation
func TestConfig_Validate_OrchestratorBounds(t *testing.T) {
	tests := []struct {
		name         string
		orchestrator *OrchestratorConfig
		wantErr      bool
		errSubstr    string
	}{
		{
			name:         "nil orchestrator is valid",
			orchestrator: nil,
			wantErr:      false,
		},
		{
			name: "max_concurrent = 1 is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 1,
			},
			wantErr: false,
		},
		{
			name: "max_concurrent > 1 is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 5,
			},
			wantErr: false,
		},
		{
			name: "max_concurrent = 0 is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 0,
			},
			wantErr:   true,
			errSubstr: "orchestrator.max_concurrent must be >= 1",
		},
		{
			name: "max_concurrent < 0 is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: -1,
			},
			wantErr:   true,
			errSubstr: "orchestrator.max_concurrent must be >= 1",
		},
		{
			name: "sequential execution mode is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode: "sequential",
				},
			},
			wantErr: false,
		},
		{
			name: "parallel execution mode is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode: "parallel",
				},
			},
			wantErr: false,
		},
		{
			name: "auto execution mode is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode: "auto",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid execution mode",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode: "invalid",
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.mode must be 'sequential', 'parallel', or 'auto'",
		},
		{
			name: "empty execution mode is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode: "",
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.mode must be 'sequential', 'parallel', or 'auto'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Orchestrator = tt.orchestrator

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_QualityBounds(t *testing.T) {
	tests := []struct {
		name      string
		quality   *quality.Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil quality config is valid",
			quality: nil,
			wantErr: false,
		},
		{
			name: "max_retries = 0 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 0,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 10 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 10,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 5 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 5,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 11 is invalid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 11,
				},
			},
			wantErr:   true,
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
		{
			name: "max_retries = -1 is invalid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: -1,
				},
			},
			wantErr:   true,
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Quality = tt.quality

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_BudgetBounds(t *testing.T) {
	tests := []struct {
		name      string
		budget    *budget.Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil budget config is valid",
			budget:  nil,
			wantErr: false,
		},
		{
			name: "disabled budget with zero daily_limit is valid",
			budget: &budget.Config{
				Enabled:    false,
				DailyLimit: 0,
			},
			wantErr: false,
		},
		{
			name: "disabled budget with negative daily_limit is valid",
			budget: &budget.Config{
				Enabled:    false,
				DailyLimit: -10,
			},
			wantErr: false,
		},
		{
			name: "enabled budget with positive daily_limit is valid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 50.0,
			},
			wantErr: false,
		},
		{
			name: "enabled budget with zero daily_limit is invalid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 0,
			},
			wantErr:   true,
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},
		{
			name: "enabled budget with negative daily_limit is invalid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: -10.5,
			},
			wantErr:   true,
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},
		{
			name: "enabled budget with very small positive daily_limit is valid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 0.01,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Budget = tt.budget

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

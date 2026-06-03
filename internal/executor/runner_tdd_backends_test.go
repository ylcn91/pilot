package executor

import "testing"

// TestResolveTDDBackends verifies that with TDD disabled or absent all four role
// backends are the identical single-backend instance (zero behavior change), and
// that a configured role gets its own backend while the others fall back to the
// single backend.
func TestResolveTDDBackends(t *testing.T) {
	tests := []struct {
		name   string
		config *BackendConfig
		// expected Name() per role.
		wantArchitect   string
		wantTestAuthor  string
		wantImplementer string
		wantQA          string
		// wantAllSingle asserts every role points at the same single backend pointer.
		wantAllSingle bool
	}{
		{
			name:            "nil TDD falls back to single backend",
			config:          &BackendConfig{Type: BackendTypeCodexExec},
			wantArchitect:   BackendTypeCodexExec,
			wantTestAuthor:  BackendTypeCodexExec,
			wantImplementer: BackendTypeCodexExec,
			wantQA:          BackendTypeCodexExec,
			wantAllSingle:   true,
		},
		{
			name: "disabled TDD falls back to single backend",
			config: &BackendConfig{
				Type: BackendTypeCodexExec,
				TDD: &TDDConfig{
					Enabled:     false,
					Implementer: &StageConfig{Type: BackendTypeOpenCode},
				},
			},
			wantArchitect:   BackendTypeCodexExec,
			wantTestAuthor:  BackendTypeCodexExec,
			wantImplementer: BackendTypeCodexExec,
			wantQA:          BackendTypeCodexExec,
			wantAllSingle:   true,
		},
		{
			name: "implementer role overrides only the implementer backend",
			config: &BackendConfig{
				Type: BackendTypeCodexExec,
				TDD: &TDDConfig{
					Enabled:     true,
					Implementer: &StageConfig{Type: BackendTypeOpenCode},
				},
			},
			wantArchitect:   BackendTypeCodexExec,
			wantTestAuthor:  BackendTypeCodexExec,
			wantImplementer: BackendTypeOpenCode,
			wantQA:          BackendTypeCodexExec,
			wantAllSingle:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRunnerWithConfig(tt.config)
			if err != nil {
				t.Fatalf("NewRunnerWithConfig: %v", err)
			}
			if got := r.architectBackend.Name(); got != tt.wantArchitect {
				t.Errorf("architectBackend = %q, want %q", got, tt.wantArchitect)
			}
			if got := r.testAuthorBackend.Name(); got != tt.wantTestAuthor {
				t.Errorf("testAuthorBackend = %q, want %q", got, tt.wantTestAuthor)
			}
			if got := r.implementerBackend.Name(); got != tt.wantImplementer {
				t.Errorf("implementerBackend = %q, want %q", got, tt.wantImplementer)
			}
			if got := r.qaBackend.Name(); got != tt.wantQA {
				t.Errorf("qaBackend = %q, want %q", got, tt.wantQA)
			}
			if tt.wantAllSingle {
				if r.architectBackend != r.backend || r.testAuthorBackend != r.backend ||
					r.implementerBackend != r.backend || r.qaBackend != r.backend {
					t.Error("with TDD off all role backends must be the single backend instance")
				}
			} else {
				// Roles without an override still reuse the single backend pointer.
				if r.architectBackend != r.backend || r.testAuthorBackend != r.backend || r.qaBackend != r.backend {
					t.Error("unconfigured roles must fall back to the single backend instance")
				}
				if r.implementerBackend == r.backend {
					t.Error("configured implementer role must be a distinct backend instance")
				}
			}
		})
	}
}

func TestResolveTDDBackendsCodexExecRoles(t *testing.T) {
	r, err := NewRunnerWithConfig(&BackendConfig{
		Type:       BackendTypeClaudeCode,
		ClaudeCode: &ClaudeCodeConfig{Command: "claude"},
		TDD: &TDDConfig{
			Enabled: true,
			Architect: &StageConfig{
				Type:   BackendTypeCodexExec,
				Model:  "gpt-5-codex",
				Effort: "high",
			},
			TestAuthor:  &StageConfig{Type: BackendTypeCodexExec},
			Implementer: &StageConfig{Type: BackendTypeCodexExec},
			QA:          &StageConfig{Type: BackendTypeCodexExec},
		},
	})
	if err != nil {
		t.Fatalf("NewRunnerWithConfig: %v", err)
	}

	for name, backend := range map[string]Backend{
		"architect":   r.architectBackend,
		"test-author": r.testAuthorBackend,
		"implementer": r.implementerBackend,
		"qa":          r.qaBackend,
	} {
		if backend.Name() != BackendTypeCodexExec {
			t.Errorf("%s backend = %q, want %q", name, backend.Name(), BackendTypeCodexExec)
		}
		if backend == r.backend {
			t.Errorf("%s backend reused primary backend; want distinct codex-exec stage backend", name)
		}
	}

	architect, ok := r.architectBackend.(*CodexExecBackend)
	if !ok {
		t.Fatalf("architect backend type = %T, want *CodexExecBackend", r.architectBackend)
	}
	if architect.config.Model != "gpt-5-codex" {
		t.Errorf("architect codex model = %q, want gpt-5-codex", architect.config.Model)
	}
	if architect.config.Effort != "high" {
		t.Errorf("architect codex effort = %q, want high", architect.config.Effort)
	}
}

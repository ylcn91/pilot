package executor

import "testing"

// TestResolveStageBackends verifies that with no pipeline configured all three
// stage backends are the identical single-backend instance (zero behavior
// change), and that a configured stage gets its own backend while the others
// fall back to the single backend.
func TestResolveStageBackends(t *testing.T) {
	tests := []struct {
		name string
		// config drives runner construction. Type is the run's primary backend.
		config *BackendConfig
		// wantPlanName/wantExecName/wantReviewName are the expected Name() per stage.
		wantPlanName   string
		wantExecName   string
		wantReviewName string
		// wantAllSingle asserts every stage points at the same single backend pointer.
		wantAllSingle bool
	}{
		{
			name:           "nil pipeline falls back to single backend",
			config:         &BackendConfig{Type: BackendTypeCodexExec},
			wantPlanName:   BackendTypeCodexExec,
			wantExecName:   BackendTypeCodexExec,
			wantReviewName: BackendTypeCodexExec,
			wantAllSingle:  true,
		},
		{
			name: "execute stage overrides only the exec backend",
			config: &BackendConfig{
				Type:     BackendTypeCodexExec,
				Pipeline: &PipelineConfig{Execute: &StageConfig{Type: BackendTypeOpenCode}},
			},
			wantPlanName:   BackendTypeCodexExec,
			wantExecName:   BackendTypeOpenCode,
			wantReviewName: BackendTypeCodexExec,
			wantAllSingle:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRunnerWithConfig(tt.config)
			if err != nil {
				t.Fatalf("NewRunnerWithConfig: %v", err)
			}
			if got := r.planBackend.Name(); got != tt.wantPlanName {
				t.Errorf("planBackend = %q, want %q", got, tt.wantPlanName)
			}
			if got := r.execBackend.Name(); got != tt.wantExecName {
				t.Errorf("execBackend = %q, want %q", got, tt.wantExecName)
			}
			if got := r.reviewBackend.Name(); got != tt.wantReviewName {
				t.Errorf("reviewBackend = %q, want %q", got, tt.wantReviewName)
			}
			if tt.wantAllSingle {
				if r.planBackend != r.backend || r.execBackend != r.backend || r.reviewBackend != r.backend {
					t.Error("with no pipeline all stage backends must be the single backend instance")
				}
			} else {
				// Stages without an override still reuse the single backend pointer.
				if r.planBackend != r.backend || r.reviewBackend != r.backend {
					t.Error("unconfigured stages must fall back to the single backend instance")
				}
				if r.execBackend == r.backend {
					t.Error("configured execute stage must be a distinct backend instance")
				}
			}
		})
	}
}

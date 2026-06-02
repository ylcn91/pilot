package executor

import (
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	runner := NewRunner()

	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}
	if runner.running == nil {
		t.Error("running map not initialized")
	}
	if runner.backend == nil {
		t.Error("backend not initialized")
	}
	if runner.backend.Name() != BackendTypeCodexExec {
		t.Errorf("default backend = %q, want %q", runner.backend.Name(), BackendTypeCodexExec)
	}
}

func TestNewRunnerWithBackend(t *testing.T) {
	backend := NewOpenCodeBackend(nil)
	runner := NewRunnerWithBackend(backend)

	if runner == nil {
		t.Fatal("NewRunnerWithBackend returned nil")
	}
	if runner.backend.Name() != BackendTypeOpenCode {
		t.Errorf("backend = %q, want %q", runner.backend.Name(), BackendTypeOpenCode)
	}
	if runner.IssueCreationEnabled() {
		t.Error("issue creation should be disabled by default")
	}

	enabledRunner, err := NewRunnerWithConfig(&BackendConfig{
		Type:            BackendTypeCodexExec,
		CreateSubIssues: true,
	})
	if err != nil {
		t.Fatalf("unexpected error creating enabled runner: %v", err)
	}
	if !enabledRunner.IssueCreationEnabled() {
		t.Error("issue creation should be enabled when config.create_sub_issues is true")
	}
}

func TestNewRunnerWithBackendNil(t *testing.T) {
	runner := NewRunnerWithBackend(nil)

	if runner == nil {
		t.Fatal("NewRunnerWithBackend returned nil")
	}
	// Should default to Codex Exec
	if runner.backend.Name() != BackendTypeCodexExec {
		t.Errorf("backend = %q, want %q", runner.backend.Name(), BackendTypeCodexExec)
	}
}

func TestNewRunnerWithConfig(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeOpenCode,
		OpenCode: &OpenCodeConfig{
			ServerURL: "http://localhost:5000",
		},
	}

	runner, err := NewRunnerWithConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner == nil {
		t.Fatal("NewRunnerWithConfig returned nil")
	}
	if runner.backend.Name() != BackendTypeOpenCode {
		t.Errorf("backend = %q, want %q", runner.backend.Name(), BackendTypeOpenCode)
	}
}

func TestSelfReviewTimeout(t *testing.T) {
	t.Run("opencode uses longer self-review timeout", func(t *testing.T) {
		runner, err := NewRunnerWithConfig(&BackendConfig{
			Type: BackendTypeOpenCode,
			OpenCode: &OpenCodeConfig{
				ServerURL: "http://localhost:5000",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := runner.selfReviewTimeout(); got != 10*time.Minute {
			t.Fatalf("selfReviewTimeout() = %v, want %v", got, 10*time.Minute)
		}
	})

	t.Run("claude-code keeps short self-review timeout", func(t *testing.T) {
		runner, err := NewRunnerWithConfig(&BackendConfig{Type: BackendTypeClaudeCode})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := runner.selfReviewTimeout(); got != 2*time.Minute {
			t.Fatalf("selfReviewTimeout() = %v, want %v", got, 2*time.Minute)
		}
	})

	t.Run("default backend keeps short self-review timeout", func(t *testing.T) {
		r := &Runner{}
		if got := r.selfReviewTimeout(); got != 2*time.Minute {
			t.Fatalf("selfReviewTimeout() = %v, want %v", got, 2*time.Minute)
		}
	})
}

func TestNewRunnerWithConfigInvalid(t *testing.T) {
	config := &BackendConfig{
		Type: "invalid-backend",
	}

	_, err := NewRunnerWithConfig(config)
	if err == nil {
		t.Error("expected error for invalid backend type")
	}
}

func TestRunnerSetBackend(t *testing.T) {
	runner := NewRunner()
	if runner.backend.Name() != BackendTypeCodexExec {
		t.Errorf("initial backend = %q, want %q", runner.backend.Name(), BackendTypeCodexExec)
	}

	opencode := NewOpenCodeBackend(nil)
	runner.SetBackend(opencode)

	if runner.backend.Name() != BackendTypeOpenCode {
		t.Errorf("backend after set = %q, want %q", runner.backend.Name(), BackendTypeOpenCode)
	}
}

func TestRunnerGetBackend(t *testing.T) {
	runner := NewRunner()
	backend := runner.GetBackend()

	if backend == nil {
		t.Fatal("GetBackend returned nil")
	}
	if backend.Name() != BackendTypeCodexExec {
		t.Errorf("backend = %q, want %q", backend.Name(), BackendTypeCodexExec)
	}
}

func TestIsRunning(t *testing.T) {
	runner := NewRunner()

	if runner.IsRunning("nonexistent") {
		t.Error("IsRunning returned true for nonexistent task")
	}
}

func TestRunnerSetRecordingsPath(t *testing.T) {
	runner := NewRunner()

	runner.SetRecordingsPath("/custom/recordings")

	if runner.recordingsPath != "/custom/recordings" {
		t.Errorf("recordingsPath = %q, want /custom/recordings", runner.recordingsPath)
	}
}

func TestRunnerSetRecordingEnabled(t *testing.T) {
	runner := NewRunner()

	// Default should be true
	if !runner.enableRecording {
		t.Error("enableRecording should default to true")
	}

	runner.SetRecordingEnabled(false)

	if runner.enableRecording {
		t.Error("enableRecording should be false after SetRecordingEnabled(false)")
	}
}

// TestRunnerFallbackModelName verifies that the telemetry fallback model name
// reflects the configured backend, not a hardcoded default. GH-2428.
func TestRunnerFallbackModelName(t *testing.T) {
	tests := []struct {
		name string
		cfg  *BackendConfig
		want string
	}{
		{
			name: "default model overrides everything",
			cfg: &BackendConfig{
				Type:         BackendTypeOpenCode,
				DefaultModel: "glm-5.1",
				OpenCode:     &OpenCodeConfig{Model: "anthropic/claude-sonnet-4-6"},
			},
			want: "glm-5.1",
		},
		{
			name: "opencode falls back to OpenCode.Model",
			cfg: &BackendConfig{
				Type:     BackendTypeOpenCode,
				OpenCode: &OpenCodeConfig{Model: "anthropic/claude-sonnet-4-6"},
			},
			want: "anthropic/claude-sonnet-4-6",
		},
		{
			name: "claude-code with no DefaultModel falls back to backend type",
			cfg:  &BackendConfig{Type: BackendTypeClaudeCode},
			want: BackendTypeClaudeCode,
		},
		{
			name: "nil config returns codex-exec default",
			cfg:  nil,
			want: BackendTypeCodexExec,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Runner{config: tt.cfg}
			if got := r.fallbackModelName(); got != tt.want {
				t.Errorf("fallbackModelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

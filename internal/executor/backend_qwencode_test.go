package executor

import (
	"testing"
)

func TestNewQwenCodeBackend(t *testing.T) {
	tests := []struct {
		name          string
		config        *QwenCodeConfig
		expectCommand string
	}{
		{
			name:          "nil config uses defaults",
			config:        nil,
			expectCommand: "qwen",
		},
		{
			name:          "empty command uses default",
			config:        &QwenCodeConfig{Command: ""},
			expectCommand: "qwen",
		},
		{
			name:          "custom command",
			config:        &QwenCodeConfig{Command: "/custom/qwen"},
			expectCommand: "/custom/qwen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewQwenCodeBackend(tt.config)
			if backend == nil {
				t.Fatal("NewQwenCodeBackend returned nil")
			}
			if backend.config.Command != tt.expectCommand {
				t.Errorf("Command = %q, want %q", backend.config.Command, tt.expectCommand)
			}
		})
	}
}

func TestQwenCodeBackendName(t *testing.T) {
	backend := NewQwenCodeBackend(nil)
	if backend.Name() != BackendTypeQwenCode {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeQwenCode)
	}
}

func TestQwenCodeBackendIsAvailable(t *testing.T) {
	backend := NewQwenCodeBackend(&QwenCodeConfig{
		Command: "/nonexistent/path/to/qwen",
	})

	if backend.IsAvailable() {
		t.Error("IsAvailable() should return false for non-existent command")
	}
}

func TestQwenCodeBuildArgs(t *testing.T) {
	tests := []struct {
		name       string
		config     *QwenCodeConfig
		opts       ExecuteOptions
		expectArgs []string
		notExpect  []string
	}{
		{
			name:   "basic prompt",
			config: &QwenCodeConfig{Command: "qwen"},
			opts: ExecuteOptions{
				Prompt:      "fix the bug",
				ProjectPath: "/project",
			},
			expectArgs: []string{"-p", "fix the bug", "--output-format", "stream-json", "--yolo"},
			notExpect:  []string{"--model", "--resume", "--verbose", "--dangerously-skip-permissions", "--effort"},
		},
		{
			name:   "with model",
			config: &QwenCodeConfig{Command: "qwen"},
			opts: ExecuteOptions{
				Prompt: "fix the bug",
				Model:  "qwen3-coder-plus",
			},
			expectArgs: []string{"--model", "qwen3-coder-plus", "--yolo"},
		},
		{
			name:   "with resume",
			config: &QwenCodeConfig{Command: "qwen", UseSessionResume: true},
			opts: ExecuteOptions{
				Prompt:          "continue",
				ResumeSessionID: "sess-abc",
			},
			expectArgs: []string{"--resume", "sess-abc"},
		},
		{
			name:   "resume disabled in config",
			config: &QwenCodeConfig{Command: "qwen", UseSessionResume: false},
			opts: ExecuteOptions{
				Prompt:          "continue",
				ResumeSessionID: "sess-abc",
			},
			notExpect: []string{"--resume"},
		},
		{
			name:   "effort silently ignored",
			config: &QwenCodeConfig{Command: "qwen"},
			opts: ExecuteOptions{
				Prompt: "fix bug",
				Effort: "max",
			},
			notExpect: []string{"--effort"},
		},
		{
			name:   "from-pr silently ignored",
			config: &QwenCodeConfig{Command: "qwen"},
			opts: ExecuteOptions{
				Prompt: "fix CI",
				FromPR: 42,
			},
			notExpect: []string{"--from-pr"},
		},
		{
			name:   "extra args appended",
			config: &QwenCodeConfig{Command: "qwen", ExtraArgs: []string{"--debug"}},
			opts: ExecuteOptions{
				Prompt: "test",
			},
			expectArgs: []string{"--debug"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewQwenCodeBackend(tt.config)
			args := backend.buildArgs(tt.opts)

			for _, expected := range tt.expectArgs {
				found := false
				for _, arg := range args {
					if arg == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("args missing %q, got %v", expected, args)
				}
			}

			for _, notExpected := range tt.notExpect {
				for _, arg := range args {
					if arg == notExpected {
						t.Errorf("args should not contain %q, got %v", notExpected, args)
						break
					}
				}
			}
		})
	}
}

func TestBackendFactoryQwenCode(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeQwenCode,
		QwenCode: &QwenCodeConfig{
			Command: "qwen",
		},
	}

	backend, err := NewBackend(config)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if backend == nil {
		t.Fatal("NewBackend() returned nil")
	}
	if backend.Name() != BackendTypeQwenCode {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeQwenCode)
	}
}

func TestBackendFactoryQwenCodeNilConfig(t *testing.T) {
	config := &BackendConfig{
		Type: BackendTypeQwenCode,
		// QwenCode is nil — should use defaults
	}

	backend, err := NewBackend(config)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	if backend == nil {
		t.Fatal("NewBackend() returned nil")
	}
	if backend.Name() != BackendTypeQwenCode {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeQwenCode)
	}
}

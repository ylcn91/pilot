package executor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeCodeConfigContextWindow(t *testing.T) {
	// GH-2163: Verify 1M context and max output tokens config fields
	tests := []struct {
		name                string
		config              *ClaudeCodeConfig
		expectDisable1M     bool
		expectMaxOutput     int
		expectEnvContains   []string
		expectEnvNotContain []string
	}{
		{
			name:                "default config - no context window flags",
			config:              &ClaudeCodeConfig{Command: "claude"},
			expectDisable1M:     false,
			expectMaxOutput:     0,
			expectEnvNotContain: []string{"CLAUDE_CODE_DISABLE_1M_CONTEXT", "CLAUDE_CODE_MAX_OUTPUT_TOKENS"},
		},
		{
			name:              "disable 1M context",
			config:            &ClaudeCodeConfig{Command: "claude", Disable1MContext: true},
			expectDisable1M:   true,
			expectEnvContains: []string{"CLAUDE_CODE_DISABLE_1M_CONTEXT=1"},
		},
		{
			name:              "max output tokens set",
			config:            &ClaudeCodeConfig{Command: "claude", MaxOutputTokens: 128000},
			expectMaxOutput:   128000,
			expectEnvContains: []string{"CLAUDE_CODE_MAX_OUTPUT_TOKENS=128000"},
		},
		{
			name:            "both flags set",
			config:          &ClaudeCodeConfig{Command: "claude", Disable1MContext: true, MaxOutputTokens: 64000},
			expectDisable1M: true,
			expectMaxOutput: 64000,
			expectEnvContains: []string{
				"CLAUDE_CODE_DISABLE_1M_CONTEXT=1",
				"CLAUDE_CODE_MAX_OUTPUT_TOKENS=64000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Disable1MContext != tt.expectDisable1M {
				t.Errorf("Disable1MContext = %v, want %v", tt.config.Disable1MContext, tt.expectDisable1M)
			}
			if tt.config.MaxOutputTokens != tt.expectMaxOutput {
				t.Errorf("MaxOutputTokens = %d, want %d", tt.config.MaxOutputTokens, tt.expectMaxOutput)
			}

			// Simulate the env-building logic from Execute()
			var env []string
			if tt.config.Disable1MContext || tt.config.MaxOutputTokens > 0 {
				env = []string{"PATH=/usr/bin"} // minimal base env for test
				if tt.config.Disable1MContext {
					env = append(env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
				}
				if tt.config.MaxOutputTokens > 0 {
					env = append(env, fmt.Sprintf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=%d", tt.config.MaxOutputTokens))
				}
			}

			for _, expected := range tt.expectEnvContains {
				found := false
				for _, e := range env {
					if e == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("env should contain %q, got %v", expected, env)
				}
			}
			for _, notExpected := range tt.expectEnvNotContain {
				for _, e := range env {
					if len(e) >= len(notExpected) && e[:len(notExpected)] == notExpected {
						t.Errorf("env should NOT contain %q, got %v", notExpected, env)
					}
				}
			}
		})
	}
}

// TestSetProviderEnv verifies the GH-2371 provider routing fields are stored
// on the backend and surface in the subprocess env build. The env-build logic
// in Execute() is mirrored here to avoid spawning a real claude CLI.
func TestSetProviderEnv(t *testing.T) {
	tests := []struct {
		name              string
		baseURL           string
		authToken         string
		model             string
		expectContains    []string
		expectNotContains []string
	}{
		{
			name:              "all empty - no injection (Anthropic default)",
			expectNotContains: []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"},
		},
		{
			name:              "base URL only",
			baseURL:           "https://api.z.ai/api/anthropic",
			expectContains:    []string{"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic"},
			expectNotContains: []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"},
		},
		{
			name:      "all three set (Z.AI)",
			baseURL:   "https://api.z.ai/api/anthropic",
			authToken: "zai-fake-token",
			model:     "glm-4.6",
			expectContains: []string{
				"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
				"ANTHROPIC_AUTH_TOKEN=zai-fake-token",
				"ANTHROPIC_MODEL=glm-4.6",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewClaudeCodeBackend(nil)
			b.SetProviderEnv(tt.baseURL, tt.authToken, tt.model)

			if b.apiBaseURL != tt.baseURL {
				t.Errorf("apiBaseURL = %q, want %q", b.apiBaseURL, tt.baseURL)
			}
			if b.apiAuthToken != tt.authToken {
				t.Errorf("apiAuthToken = %q, want %q", b.apiAuthToken, tt.authToken)
			}
			if b.defaultModel != tt.model {
				t.Errorf("defaultModel = %q, want %q", b.defaultModel, tt.model)
			}

			// Mirror Execute()'s env-build logic.
			env := []string{"PATH=/usr/bin", "PILOT_EXECUTOR=1"}
			if b.apiBaseURL != "" {
				env = append(env, "ANTHROPIC_BASE_URL="+b.apiBaseURL)
			}
			if b.apiAuthToken != "" {
				env = append(env, "ANTHROPIC_AUTH_TOKEN="+b.apiAuthToken)
			}
			if b.defaultModel != "" {
				env = append(env, "ANTHROPIC_MODEL="+b.defaultModel)
			}

			for _, want := range tt.expectContains {
				found := false
				for _, e := range env {
					if e == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("env should contain %q, got %v", want, env)
				}
			}
			for _, banned := range tt.expectNotContains {
				for _, e := range env {
					if len(e) >= len(banned) && e[:len(banned)] == banned {
						t.Errorf("env should NOT contain prefix %q, got %v", banned, env)
					}
				}
			}
		})
	}
}

// TestNewBackendWiresProviderEnv verifies the factory propagates
// BackendConfig.{APIBaseURL,APIAuthToken,DefaultModel} onto the
// ClaudeCodeBackend so users configure provider routing once (GH-2371).
func TestNewBackendWiresProviderEnv(t *testing.T) {
	cfg := &BackendConfig{
		Type:         BackendTypeClaudeCode,
		APIBaseURL:   "https://api.z.ai/api/anthropic",
		APIAuthToken: "zai-fake-token",
		DefaultModel: "glm-4.6",
		ClaudeCode:   &ClaudeCodeConfig{Command: "claude"},
	}

	backend, err := NewBackend(cfg)
	if err != nil {
		t.Fatalf("NewBackend error: %v", err)
	}
	cc, ok := backend.(*ClaudeCodeBackend)
	if !ok {
		t.Fatalf("expected *ClaudeCodeBackend, got %T", backend)
	}
	if cc.apiBaseURL != cfg.APIBaseURL {
		t.Errorf("apiBaseURL = %q, want %q", cc.apiBaseURL, cfg.APIBaseURL)
	}
	if cc.apiAuthToken != cfg.APIAuthToken {
		t.Errorf("apiAuthToken = %q, want %q", cc.apiAuthToken, cfg.APIAuthToken)
	}
	if cc.defaultModel != cfg.DefaultModel {
		t.Errorf("defaultModel = %q, want %q", cc.defaultModel, cfg.DefaultModel)
	}
}

// TestClaudeCodeBackendResumeSessionFallback verifies the GH-2377 fix:
// when Execute is called with ResumeSessionID set and the CLI exits with a
// "session not found" stderr, the backend retries Execute without --resume.
//
// Strategy: install a tiny shell script as the "claude" command that writes
// its full argv to a counter file, then exits with different stderr on the
// first vs second invocation. If the fallback fires, we expect exactly 2
// invocations — the first containing "--resume" and the second not.
func TestClaudeCodeBackendResumeSessionFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-CLI test relies on shell scripts; skipping on windows")
	}

	tmpDir := t.TempDir()
	logFile := tmpDir + "/calls.log"
	script := tmpDir + "/fake-claude"

	// Script logs argv and exits with different stderr based on call count.
	body := `#!/bin/sh
printf '%s\n' "$*" >> ` + logFile + `
COUNT=$(wc -l < ` + logFile + ` | tr -d ' ')
if [ "$COUNT" = "1" ]; then
  echo "No conversation found with session ID: abc-123" >&2
  exit 1
fi
echo "fake retry stderr" >&2
exit 2
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	backend := NewClaudeCodeBackend(&ClaudeCodeConfig{Command: script})
	opts := ExecuteOptions{
		Prompt:          "hello",
		ProjectPath:     tmpDir,
		ResumeSessionID: "abc-123",
		EventHandler:    func(BackendEvent) {},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := backend.Execute(ctx, opts)
	if err == nil {
		t.Fatal("expected error from fake CLI, got nil")
	}

	// Verify exactly 2 invocations and --resume dropped on retry.
	data, readErr := os.ReadFile(logFile)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 CLI invocations, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "--resume") || !strings.Contains(lines[0], "abc-123") {
		t.Errorf("first invocation missing --resume: %q", lines[0])
	}
	if strings.Contains(lines[1], "--resume") {
		t.Errorf("second invocation should not contain --resume: %q", lines[1])
	}
}

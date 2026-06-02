package executor

import (
	"log/slog"
	"slices"
	"testing"
)

func newArgsTestBackend(cfg *ClaudeCodeConfig) *ClaudeCodeBackend {
	if cfg == nil {
		cfg = &ClaudeCodeConfig{Command: "claude"}
	}
	return &ClaudeCodeBackend{config: cfg, log: slog.Default()}
}

// argValue returns the element following the first occurrence of flag.
func argValue(args []string, flag string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

func TestBuildExecArgs_FromPR(t *testing.T) {
	b := newArgsTestBackend(&ClaudeCodeConfig{Command: "claude", UseFromPR: true})
	args := b.buildExecArgs(ExecuteOptions{FromPR: 42, Prompt: "do it"}, true)

	if len(args) == 0 || args[0] != "--from-pr" {
		t.Fatalf("expected --from-pr first, got %v", args)
	}
	if v, _ := argValue(args, "--from-pr"); v != "42" {
		t.Errorf("--from-pr value = %q, want 42", v)
	}
	if v, _ := argValue(args, "-p"); v != "do it" {
		t.Errorf("-p value = %q, want %q", v, "do it")
	}
	if slices.Contains(args, "--resume") {
		t.Errorf("did not expect --resume, got %v", args)
	}
	for _, want := range []string{"--verbose", "--output-format", "stream-json", "--dangerously-skip-permissions"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in %v", want, args)
		}
	}
}

func TestBuildExecArgs_FromPRGatedOff(t *testing.T) {
	tests := []struct {
		name        string
		useFromPR   bool
		allowFromPR bool
	}{
		{"allowFromPR false", true, false},
		{"config UseFromPR false", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newArgsTestBackend(&ClaudeCodeConfig{Command: "claude", UseFromPR: tt.useFromPR})
			args := b.buildExecArgs(ExecuteOptions{FromPR: 7, Prompt: "p"}, tt.allowFromPR)
			if slices.Contains(args, "--from-pr") {
				t.Errorf("expected no --from-pr, got %v", args)
			}
			if args[0] != "-p" {
				t.Errorf("expected -p first (plain mode), got %v", args)
			}
		})
	}
}

func TestBuildExecArgs_Resume(t *testing.T) {
	b := newArgsTestBackend(nil)
	args := b.buildExecArgs(ExecuteOptions{ResumeSessionID: "sess-1", Prompt: "p"}, true)
	if args[0] != "--resume" {
		t.Fatalf("expected --resume first, got %v", args)
	}
	if v, _ := argValue(args, "--resume"); v != "sess-1" {
		t.Errorf("--resume value = %q, want sess-1", v)
	}
	if slices.Contains(args, "--from-pr") {
		t.Errorf("did not expect --from-pr, got %v", args)
	}
}

func TestBuildExecArgs_FromPRWinsOverResume(t *testing.T) {
	b := newArgsTestBackend(&ClaudeCodeConfig{Command: "claude", UseFromPR: true})
	args := b.buildExecArgs(ExecuteOptions{FromPR: 9, ResumeSessionID: "sess", Prompt: "p"}, true)
	if args[0] != "--from-pr" {
		t.Fatalf("--from-pr should take precedence, got %v", args)
	}
	if slices.Contains(args, "--resume") {
		t.Errorf("did not expect --resume when --from-pr active, got %v", args)
	}
}

func TestBuildExecArgs_Plain(t *testing.T) {
	b := newArgsTestBackend(nil)
	args := b.buildExecArgs(ExecuteOptions{Prompt: "hello"}, true)
	if args[0] != "-p" {
		t.Fatalf("expected -p first, got %v", args)
	}
	if v, _ := argValue(args, "-p"); v != "hello" {
		t.Errorf("-p value = %q, want hello", v)
	}
}

func TestBuildExecArgs_OptionalFlags(t *testing.T) {
	b := newArgsTestBackend(&ClaudeCodeConfig{Command: "claude", ExtraArgs: []string{"--extra", "x"}})
	args := b.buildExecArgs(ExecuteOptions{
		Prompt:        "p",
		Model:         "opus",
		MaxTurns:      3,
		Effort:        "high",
		AllowedTools:  []string{"Read", "Edit"},
		MCPConfigPath: "/tmp/mcp.json",
	}, true)

	checks := map[string]string{
		"--model":        "opus",
		"--max-turns":    "3",
		"--effort":       "high",
		"--allowedTools": "Read,Edit",
		"--mcp-config":   "/tmp/mcp.json",
	}
	for flag, want := range checks {
		if v, ok := argValue(args, flag); !ok || v != want {
			t.Errorf("%s = %q (found=%v), want %q", flag, v, ok, want)
		}
	}
	// ExtraArgs appended last.
	if n := len(args); n < 2 || args[n-2] != "--extra" || args[n-1] != "x" {
		t.Errorf("ExtraArgs not appended last, got %v", args)
	}
}

func TestBuildExecArgs_OmitsUnsetOptionalFlags(t *testing.T) {
	b := newArgsTestBackend(nil)
	args := b.buildExecArgs(ExecuteOptions{Prompt: "p"}, true)
	for _, flag := range []string{"--model", "--max-turns", "--effort", "--allowedTools", "--mcp-config"} {
		if slices.Contains(args, flag) {
			t.Errorf("unexpected %q for empty opts: %v", flag, args)
		}
	}
}

func TestBuildExecEnv(t *testing.T) {
	b := &ClaudeCodeBackend{
		config:       &ClaudeCodeConfig{Command: "claude", Disable1MContext: true, MaxOutputTokens: 1234},
		log:          slog.Default(),
		apiBaseURL:   "https://example.test",
		apiAuthToken: "fake-auth-token",
		defaultModel: "opus",
	}
	env := b.buildExecEnv()
	want := []string{
		"PILOT_EXECUTOR=1",
		"ANTHROPIC_BASE_URL=https://example.test",
		"ANTHROPIC_AUTH_TOKEN=fake-auth-token",
		"ANTHROPIC_MODEL=opus",
		"CLAUDE_CODE_DISABLE_1M_CONTEXT=1",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=1234",
	}
	for _, w := range want {
		if !slices.Contains(env, w) {
			t.Errorf("env missing %q", w)
		}
	}
}

func TestBuildExecEnv_Defaults(t *testing.T) {
	b := newArgsTestBackend(nil)
	env := b.buildExecEnv()
	if !slices.Contains(env, "PILOT_EXECUTOR=1") {
		t.Error("PILOT_EXECUTOR=1 should always be set")
	}
	for _, unexpected := range []string{"ANTHROPIC_BASE_URL=", "ANTHROPIC_AUTH_TOKEN=", "ANTHROPIC_MODEL=", "CLAUDE_CODE_DISABLE_1M_CONTEXT=1"} {
		for _, e := range env {
			if e == unexpected || (len(unexpected) > 0 && unexpected[len(unexpected)-1] == '=' && len(e) >= len(unexpected) && e[:len(unexpected)] == unexpected) {
				t.Errorf("did not expect env starting with %q, got %q", unexpected, e)
			}
		}
	}
}

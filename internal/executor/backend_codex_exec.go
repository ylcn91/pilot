package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

type CodexExecBackend struct {
	config           *CodexExecConfig
	heartbeatTimeout time.Duration
	log              *slog.Logger
}

func NewCodexExecBackend(config *CodexExecConfig) *CodexExecBackend {
	if config == nil {
		config = &CodexExecConfig{Command: "codex", Sandbox: "workspace-write"}
	}
	if config.Command == "" {
		config.Command = "codex"
	}
	if config.Sandbox == "" && !config.BypassApprovalsAndSandbox {
		config.Sandbox = "workspace-write"
	}
	return &CodexExecBackend{
		config:           config,
		heartbeatTimeout: DefaultHeartbeatTimeout,
		log:              logging.WithComponent("executor.codexexec"),
	}
}

func (b *CodexExecBackend) SetHeartbeatTimeout(d time.Duration) {
	b.heartbeatTimeout = d
}

func (b *CodexExecBackend) Name() string {
	return BackendTypeCodexExec
}

func (b *CodexExecBackend) IsAvailable() bool {
	path, err := exec.LookPath(b.config.Command)
	if err != nil {
		return false
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		b.log.Warn("codex-exec: could not determine version", "error", err)
		return true
	}
	b.log.Info("codex-exec: detected version", "version", strings.TrimSpace(string(out)))
	return true
}

func (b *CodexExecBackend) buildArgs(opts ExecuteOptions) []string {
	if opts.ResumeSessionID != "" && b.config.UseSessionResume {
		return b.buildResumeArgs(opts)
	}
	return b.buildInitialArgs(opts)
}

func (b *CodexExecBackend) buildInitialArgs(opts ExecuteOptions) []string {
	args := []string{"exec", "--json"}

	if opts.ProjectPath != "" {
		args = append(args, "-C", opts.ProjectPath)
	}
	if b.config.BypassApprovalsAndSandbox {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else if b.config.Sandbox != "" {
		args = append(args, "--sandbox", b.config.Sandbox)
	}
	args = b.appendCommonArgs(args, opts)
	args = append(args, opts.Prompt)
	return args
}

func (b *CodexExecBackend) buildResumeArgs(opts ExecuteOptions) []string {
	args := []string{"exec", "resume", "--json"}
	if b.config.BypassApprovalsAndSandbox {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	args = b.appendCommonArgs(args, opts)
	args = append(args, opts.ResumeSessionID, opts.Prompt)
	return args
}

func (b *CodexExecBackend) appendCommonArgs(args []string, opts ExecuteOptions) []string {
	if model := firstNonEmpty(opts.Model, b.config.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := firstNonEmpty(opts.Effort, b.config.Effort); effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	if b.config.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if b.config.OutputSchemaPath != "" {
		args = append(args, "--output-schema", b.config.OutputSchemaPath)
	}
	args = append(args, b.config.ExtraArgs...)
	return args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// buildCmd constructs the codex-exec command for the given options.
//
// Stdin is set to an explicit zero-length reader so codex reads instant EOF
// in every launch context (direct CLI, daemon, app-server, bench). Without
// this, codex can block on an open (non-EOF) stdin inherited from the parent
// process, printing "Reading additional input from stdin..." and hanging
// headless. Setting it explicitly (rather than relying on the nil-implicit
// behaviour) prevents a future parent that holds stdin open from re-triggering
// the hang.
func (b *CodexExecBackend) buildCmd(ctx context.Context, opts ExecuteOptions) *exec.Cmd {
	args := b.buildArgs(opts)

	cmd := exec.CommandContext(ctx, b.config.Command, args...)
	cmd.Dir = opts.ProjectPath
	cmd.Stdin = bytes.NewReader(nil)

	return cmd
}

func (b *CodexExecBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	cmd := b.buildCmd(ctx, opts)

	b.log.Debug("Starting Codex exec",
		slog.String("command", b.config.Command),
		slog.String("project", opts.ProjectPath),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Codex exec: %w", err)
	}
	b.log.Debug("Codex exec started", slog.Int("pid", cmd.Process.Pid))

	result := &BackendResult{}
	var stderrOutput strings.Builder
	var wg sync.WaitGroup
	cmdDone := make(chan struct{})

	var lastEventAt atomic.Int64
	lastEventAt.Store(time.Now().UnixNano())

	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()
	go b.monitorHeartbeat(heartbeatCtx, cmdDone, cmd, opts, &lastEventAt)
	b.monitorWatchdog(cmdDone, cmd, opts)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.Recover("executor.codexexec.stdout")
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			lastEventAt.Store(time.Now().UnixNano())

			if opts.Verbose {
				fmt.Printf("   %s\n", line)
			}

			event := b.parseStreamEvent(line)
			if opts.EventHandler != nil {
				opts.EventHandler(event)
			}

			switch event.Type {
			case EventTypeInit:
				if event.SessionID != "" {
					result.SessionID = event.SessionID
				}
			case EventTypeText:
				if event.Message != "" {
					result.LastAssistantText = event.Message
				}
			case EventTypeResult:
				result.SawSuccessResult = !event.IsError
				if event.IsError {
					result.Error = event.Message
				} else if event.Message != "" {
					result.Output = event.Message
				} else if result.LastAssistantText != "" {
					result.Output = result.LastAssistantText
				}
			case EventTypeError:
				result.Error = event.Message
			}

			result.TokensInput += event.TokensInput
			result.TokensOutput += event.TokensOutput
			result.CacheReadInputTokens += event.CacheReadInputTokens
			if event.Model != "" {
				result.Model = event.Model
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.Recover("executor.codexexec.stderr")
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line + "\n")
			if opts.Verbose {
				fmt.Printf("   [err] %s\n", line)
			}
		}
	}()

	go b.monitorContext(ctx, cmdDone, cmd)

	wg.Wait()

	err = cmd.Wait()
	close(cmdDone)
	result.Stderr = stderrOutput.String()

	if err != nil {
		result.Success = false
		codexErr := classifyCodexExecError(result.Stderr, err)
		result.ErrorType = string(codexErr.Type)
		if result.Error == "" {
			result.Error = codexErr.Error()
		}

		b.log.Warn("Codex exec failed",
			slog.String("error_type", string(codexErr.Type)),
			slog.String("message", codexErr.Message),
			slog.String("stderr", codexErr.Stderr),
		)

		if codexErr.Type == CodexExecErrorTypeSessionNotFound && opts.ResumeSessionID != "" {
			b.log.Warn("codex-exec: session not found, retrying without resume",
				"session_id", opts.ResumeSessionID)
			opts.ResumeSessionID = ""
			return b.Execute(ctx, opts)
		}

		return result, codexErr
	}

	result.Success = true
	if result.Output == "" {
		result.Output = result.LastAssistantText
	}
	return result, nil
}

func (b *CodexExecBackend) monitorHeartbeat(ctx context.Context, cmdDone <-chan struct{}, cmd *exec.Cmd, opts ExecuteOptions, lastEventAt *atomic.Int64) {
	defer logging.Recover("executor.codexexec.heartbeat")
	ticker := time.NewTicker(HeartbeatCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cmdDone:
			return
		case <-ticker.C:
			lastTime := time.Unix(0, lastEventAt.Load())
			age := time.Since(lastTime)
			if age <= b.heartbeatTimeout {
				continue
			}
			if cmd.Process == nil {
				return
			}
			b.log.Warn("Heartbeat timeout detected, killing hung Codex process",
				slog.Int("pid", cmd.Process.Pid),
				slog.Duration("last_event_age", age),
				slog.Duration("timeout", b.heartbeatTimeout),
			)
			if opts.HeartbeatCallback != nil {
				opts.HeartbeatCallback(cmd.Process.Pid, age)
			}
			if err := cmd.Process.Kill(); err != nil {
				b.log.Error("Failed to kill hung Codex process",
					slog.Int("pid", cmd.Process.Pid),
					slog.Any("error", err),
				)
			}
			return
		}
	}
}

func (b *CodexExecBackend) monitorWatchdog(cmdDone <-chan struct{}, cmd *exec.Cmd, opts ExecuteOptions) {
	if opts.WatchdogTimeout <= 0 {
		return
	}
	go func() {
		defer logging.Recover("executor.codexexec.watchdog")
		select {
		case <-cmdDone:
			return
		case <-time.After(opts.WatchdogTimeout):
			if cmd.Process == nil {
				return
			}
			b.log.Warn("Watchdog timeout expired, forcibly killing Codex process",
				slog.Int("pid", cmd.Process.Pid),
				slog.Duration("watchdog_timeout", opts.WatchdogTimeout),
			)
			if opts.WatchdogCallback != nil {
				opts.WatchdogCallback(cmd.Process.Pid, opts.WatchdogTimeout)
			}
			if err := cmd.Process.Kill(); err != nil {
				b.log.Error("Watchdog failed to kill Codex process",
					slog.Int("pid", cmd.Process.Pid),
					slog.Any("error", err),
				)
			}
		}
	}()
}

func (b *CodexExecBackend) monitorContext(ctx context.Context, cmdDone <-chan struct{}, cmd *exec.Cmd) {
	defer logging.Recover("executor.codexexec.context")
	select {
	case <-cmdDone:
		return
	case <-ctx.Done():
		if cmd.Process == nil {
			return
		}
		b.log.Warn("Context cancelled, waiting grace period before hard kill",
			slog.Int("pid", cmd.Process.Pid),
			slog.Duration("grace_period", GracePeriod),
		)
		select {
		case <-cmdDone:
			return
		case <-time.After(GracePeriod):
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil {
					b.log.Error("Failed to kill Codex process",
						slog.Int("pid", cmd.Process.Pid),
						slog.Any("error", err),
					)
				}
			}
		}
	}
}

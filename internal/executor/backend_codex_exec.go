package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
)

type CodexExecErrorType string

const (
	CodexExecErrorTypeRateLimit       CodexExecErrorType = "rate_limit"
	CodexExecErrorTypeAPIError        CodexExecErrorType = "api_error"
	CodexExecErrorTypeTimeout         CodexExecErrorType = "timeout"
	CodexExecErrorTypeInvalidConfig   CodexExecErrorType = "invalid_config"
	CodexExecErrorTypeSessionNotFound CodexExecErrorType = "session_not_found"
	CodexExecErrorTypeSandbox         CodexExecErrorType = "sandbox_error"
	CodexExecErrorTypeUnknown         CodexExecErrorType = "unknown"
)

type CodexExecError struct {
	Type    CodexExecErrorType
	Message string
	Stderr  string
}

func (e *CodexExecError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s (stderr: %s)", e.Type, e.Message, e.Stderr)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *CodexExecError) ErrorType() string { return string(e.Type) }

func (e *CodexExecError) ErrorMessage() string { return e.Message }

func (e *CodexExecError) ErrorStderr() string { return e.Stderr }

func classifyCodexExecError(stderr string, originalErr error) *CodexExecError {
	stderrLower := strings.ToLower(stderr)
	trimmed := strings.TrimSpace(stderr)

	if strings.Contains(stderrLower, "rate limit") ||
		strings.Contains(stderrLower, "usage limit") ||
		strings.Contains(stderrLower, "429") {
		return &CodexExecError{Type: CodexExecErrorTypeRateLimit, Message: "Codex rate limit reached", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "not logged in") ||
		strings.Contains(stderrLower, "unauthorized") ||
		strings.Contains(stderrLower, "authentication") ||
		strings.Contains(stderrLower, "401") ||
		strings.Contains(stderrLower, "403") {
		return &CodexExecError{Type: CodexExecErrorTypeAPIError, Message: "Codex authentication or API error", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "session not found") ||
		strings.Contains(stderrLower, "unknown thread") ||
		strings.Contains(stderrLower, "thread") && strings.Contains(stderrLower, "not found") {
		return &CodexExecError{Type: CodexExecErrorTypeSessionNotFound, Message: "Codex session not found", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "invalid model") ||
		strings.Contains(stderrLower, "unknown option") ||
		strings.Contains(stderrLower, "unrecognized") ||
		strings.Contains(stderrLower, "invalid config") {
		return &CodexExecError{Type: CodexExecErrorTypeInvalidConfig, Message: "Invalid Codex exec configuration", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "sandbox") ||
		strings.Contains(stderrLower, "approval") && strings.Contains(stderrLower, "required") {
		return &CodexExecError{Type: CodexExecErrorTypeSandbox, Message: "Codex sandbox or approval error", Stderr: trimmed}
	}
	if strings.Contains(stderrLower, "killed") ||
		strings.Contains(stderrLower, "signal") ||
		strings.Contains(stderrLower, "timeout") {
		return &CodexExecError{Type: CodexExecErrorTypeTimeout, Message: "Codex process killed or timed out", Stderr: trimmed}
	}

	msg := "Unknown error"
	if originalErr != nil {
		msg = originalErr.Error()
	}
	return &CodexExecError{Type: CodexExecErrorTypeUnknown, Message: msg, Stderr: trimmed}
}

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

func (b *CodexExecBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	args := b.buildArgs(opts)

	cmd := exec.CommandContext(ctx, b.config.Command, args...)
	cmd.Dir = opts.ProjectPath

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

type codexExecEnvelope struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     *codexExecItem  `json:"item"`
	Usage    *codexExecUsage `json:"usage"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	Msg      json.RawMessage `json:"msg"`
}

type codexExecItem struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	Command          string          `json:"command"`
	AggregatedOutput string          `json:"aggregated_output"`
	ExitCode         *int            `json:"exit_code"`
	Status           string          `json:"status"`
	SummaryText      string          `json:"summary_text"`
	RawContent       string          `json:"raw_content"`
	Changes          json.RawMessage `json:"changes"`
}

type codexExecUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	Model                 string `json:"model"`
}

func (b *CodexExecBackend) parseStreamEvent(line string) BackendEvent {
	event := BackendEvent{Raw: line}

	var envelope codexExecEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		event.Type = EventTypeText
		event.Message = line
		return event
	}
	if envelope.Type == "" && len(envelope.Msg) > 0 {
		if err := json.Unmarshal(envelope.Msg, &envelope); err != nil {
			event.Type = EventTypeText
			event.Message = line
			return event
		}
	}

	switch envelope.Type {
	case "thread.started":
		event.Type = EventTypeInit
		event.SessionID = envelope.ThreadID
		event.Message = "Codex exec initialized"
	case "turn.started", "task_started":
		event.Type = EventTypeProgress
		event.Message = "Codex turn started"
	case "turn.completed", "task_complete":
		event.Type = EventTypeResult
		event.Message = envelope.Message
	case "stream_error":
		event.Type = EventTypeError
		event.IsError = true
		event.Message = firstNonEmpty(envelope.Error, envelope.Message)
	case "item.started", "item.completed":
		event = b.parseItemEvent(event, envelope.Type, envelope.Item)
	default:
		if envelope.Message != "" {
			event.Type = EventTypeText
			event.Message = envelope.Message
		} else {
			event.Type = EventTypeProgress
			event.Message = envelope.Type
		}
	}

	if envelope.Usage != nil {
		event.TokensInput = envelope.Usage.InputTokens
		event.CacheReadInputTokens = envelope.Usage.CachedInputTokens
		event.TokensOutput = envelope.Usage.OutputTokens
		event.Model = envelope.Usage.Model
	}

	return event
}

func (b *CodexExecBackend) parseItemEvent(event BackendEvent, eventType string, item *codexExecItem) BackendEvent {
	if item == nil {
		event.Type = EventTypeProgress
		event.Message = eventType
		return event
	}

	switch item.Type {
	case "agent_message":
		event.Type = EventTypeText
		event.Message = item.Text
	case "reasoning":
		event.Type = EventTypeProgress
		event.Message = firstNonEmpty(item.SummaryText, item.RawContent, "Codex reasoning")
	case "command_execution":
		if eventType == "item.started" || item.Status == "in_progress" {
			event.Type = EventTypeToolUse
			event.ToolName = "Bash"
			event.ToolInput = map[string]interface{}{"command": item.Command}
			event.Message = "Using Bash"
		} else {
			event.Type = EventTypeToolResult
			event.ToolName = "Bash"
			event.ToolResult = item.AggregatedOutput
			event.Message = item.AggregatedOutput
			event.IsError = item.ExitCode != nil && *item.ExitCode != 0
		}
	case "file_change":
		event.Type = EventTypeToolUse
		event.ToolName = "Edit"
		event.ToolInput = map[string]interface{}{"changes": string(item.Changes)}
		event.Message = "Applying file changes"
	default:
		event.Type = EventTypeProgress
		event.Message = item.Type
	}

	return event
}

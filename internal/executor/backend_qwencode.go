package executor

import (
	"bufio"
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

// qwenToolNameMap normalizes Qwen Code tool names (snake_case) to the PascalCase
// names expected by Runner's handleToolUse() for progress phase detection.
var qwenToolNameMap = map[string]string{
	"read_file":         "Read",
	"write_file":        "Write",
	"edit":              "Edit",
	"run_shell_command": "Bash",
	"grep_search":       "Grep",
	"glob":              "Glob",
	"list_directory":    "Bash",
	"web_fetch":         "WebFetch",
	"web_search":        "WebSearch",
	"todo_write":        "TodoWrite",
	"save_memory":       "TodoWrite",
	"task":              "Task",
	"skill":             "Skill",
	"lsp":               "Bash",
	"exit_plan_mode":    "ExitPlanMode",
}

// normalizeQwenToolName maps Qwen Code snake_case tool names to Runner-compatible
// PascalCase names. MCP tools (prefixed with "mcp__") pass through unchanged.
func normalizeQwenToolName(name string) string {
	if mapped, ok := qwenToolNameMap[name]; ok {
		return mapped
	}
	return name
}

// QwenCodeBackend implements Backend for Qwen Code CLI.
// Qwen Code is a Gemini CLI fork that supports --output-format stream-json
// with a nearly identical event structure to Claude Code.
type QwenCodeBackend struct {
	config           *QwenCodeConfig
	heartbeatTimeout time.Duration
	log              *slog.Logger

	// subprocessLimits configures RSS telemetry and optional RLIMIT_AS cap. #27.
	subprocessLimits *SubprocessLimitsConfig
}

// SetSubprocessLimits configures RSS telemetry and optional memory cap for the
// Qwen Code subprocess. #27 (mirrors ClaudeCodeBackend).
func (b *QwenCodeBackend) SetSubprocessLimits(cfg *SubprocessLimitsConfig) {
	b.subprocessLimits = cfg
}

// NewQwenCodeBackend creates a new Qwen Code backend.
func NewQwenCodeBackend(config *QwenCodeConfig) *QwenCodeBackend {
	if config == nil {
		config = &QwenCodeConfig{Command: "qwen"}
	}
	if config.Command == "" {
		config.Command = "qwen"
	}
	return &QwenCodeBackend{
		config:           config,
		heartbeatTimeout: DefaultHeartbeatTimeout,
		log:              logging.WithComponent("executor.qwencode"),
	}
}

// SetHeartbeatTimeout sets a custom heartbeat timeout for this backend.
func (b *QwenCodeBackend) SetHeartbeatTimeout(d time.Duration) {
	b.heartbeatTimeout = d
}

// Name returns the backend identifier.
func (b *QwenCodeBackend) Name() string {
	return BackendTypeQwenCode
}

// IsAvailable checks if Qwen Code CLI is installed.
func (b *QwenCodeBackend) IsAvailable() bool {
	path, err := exec.LookPath(b.config.Command)
	if err != nil {
		return false
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		b.log.Warn("qwen-code: could not determine version", "error", err)
		return true
	}
	version := strings.TrimSpace(string(out))
	b.log.Info("qwen-code: detected version", "version", version)
	return true
}

// buildArgs constructs the CLI arguments for Qwen Code execution.
func (b *QwenCodeBackend) buildArgs(opts ExecuteOptions) []string {
	var args []string

	// Session resume support
	if opts.ResumeSessionID != "" && b.config.UseSessionResume {
		args = append(args, "--resume", opts.ResumeSessionID)
		b.log.Info("Resuming Qwen Code session",
			slog.String("session_id", opts.ResumeSessionID),
		)
	}

	// Core flags
	args = append(args,
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--yolo", // Qwen's equivalent of --dangerously-skip-permissions
	)

	// Model flag
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
		b.log.Info("Using routed model", slog.String("model", opts.Model))
	}

	// Note: --effort and --from-pr are not supported by Qwen Code — skip silently

	// Extra args from config
	args = append(args, b.config.ExtraArgs...)

	return args
}

// Execute runs a prompt through Qwen Code CLI.
func (b *QwenCodeBackend) Execute(ctx context.Context, opts ExecuteOptions) (*BackendResult, error) {
	args := b.buildArgs(opts)

	cmd := exec.CommandContext(ctx, b.config.Command, args...)
	cmd.Dir = opts.ProjectPath

	b.log.Debug("Starting Qwen Code",
		slog.String("command", b.config.Command),
		slog.String("project", opts.ProjectPath),
	)

	// Create pipes for output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Qwen Code: %w", err)
	}
	b.log.Debug("Qwen Code started", slog.Int("pid", cmd.Process.Pid))

	// #27: apply RSS cap (Linux: RLIMIT_AS via prlimit64; darwin/other: no-op)
	// and start the RSS sampler — collects peak/final RSS for telemetry. Mirrors
	// claude-code so OOM diagnosis works across subprocess backends.
	applyResourceLimits(cmd.Process.Pid, b.subprocessLimits)
	sampleInterval := 10 * time.Second
	if b.subprocessLimits != nil && b.subprocessLimits.SampleIntervalSec > 0 {
		sampleInterval = time.Duration(b.subprocessLimits.SampleIntervalSec) * time.Second
	}
	rssSamplerCtx, cancelRSSSampler := context.WithCancel(context.Background())
	rssCh := StartRSSSampler(rssSamplerCtx, cmd.Process.Pid, sampleInterval)

	// Track results
	result := &BackendResult{}
	var stderrOutput strings.Builder
	var wg sync.WaitGroup

	// Channel to signal command completion
	cmdDone := make(chan struct{})

	// Heartbeat tracking: store last event time as Unix nano (atomic int64)
	var lastEventAt atomic.Int64
	lastEventAt.Store(time.Now().UnixNano())

	// Heartbeat monitor goroutine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()
	go func() {
		defer logging.Recover("executor.qwencode.heartbeat")
		ticker := time.NewTicker(HeartbeatCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-cmdDone:
				return
			case <-ticker.C:
				lastNano := lastEventAt.Load()
				lastTime := time.Unix(0, lastNano)
				age := time.Since(lastTime)
				if age > b.heartbeatTimeout {
					b.log.Warn("Heartbeat timeout detected, killing hung process",
						slog.Int("pid", cmd.Process.Pid),
						slog.Duration("last_event_age", age),
						slog.Duration("timeout", b.heartbeatTimeout),
					)

					if opts.HeartbeatCallback != nil {
						opts.HeartbeatCallback(cmd.Process.Pid, age)
					}

					if cmd.Process != nil {
						if err := cmd.Process.Kill(); err != nil {
							b.log.Error("Failed to kill hung process",
								slog.Int("pid", cmd.Process.Pid),
								slog.Any("error", err),
							)
						} else {
							b.log.Info("Hung process killed successfully",
								slog.Int("pid", cmd.Process.Pid),
							)
						}
					}
					return
				}
			}
		}
	}()

	// Watchdog goroutine: hard kill after absolute timeout
	if opts.WatchdogTimeout > 0 {
		go func() {
			defer logging.Recover("executor.qwencode.watchdog")
			select {
			case <-cmdDone:
				return
			case <-time.After(opts.WatchdogTimeout):
				if cmd.Process == nil {
					return
				}

				b.log.Warn("Watchdog timeout expired, forcibly killing subprocess",
					slog.Int("pid", cmd.Process.Pid),
					slog.Duration("watchdog_timeout", opts.WatchdogTimeout),
				)

				if opts.WatchdogCallback != nil {
					opts.WatchdogCallback(cmd.Process.Pid, opts.WatchdogTimeout)
				}

				if err := cmd.Process.Kill(); err != nil {
					b.log.Error("Watchdog failed to kill process",
						slog.Int("pid", cmd.Process.Pid),
						slog.Any("error", err),
					)
				} else {
					b.log.Info("Watchdog killed process successfully",
						slog.Int("pid", cmd.Process.Pid),
					)
				}
			}
		}()
	}

	// Read stdout (stream-json events)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.Recover("executor.qwencode.stdout")
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// Update heartbeat timestamp
			lastEventAt.Store(time.Now().UnixNano())

			if opts.Verbose {
				fmt.Printf("   %s\n", line)
			}

			// Parse and convert to BackendEvent
			event := b.parseStreamEvent(line)
			if opts.EventHandler != nil {
				opts.EventHandler(event)
			}

			// GH-2777/#24: track the last assistant text block so a DECLINED
			// marker (or any refusal) emitted by Qwen is surfaced to the runner.
			if event.Type == EventTypeText && event.Message != "" {
				result.LastAssistantText = event.Message
			}

			// Track final result
			if event.Type == EventTypeResult {
				if event.IsError {
					result.Error = event.Message
				} else {
					result.Output = event.Message
				}
			}

			// Capture session ID from init event
			if event.Type == EventTypeInit && event.SessionID != "" {
				result.SessionID = event.SessionID
			}

			// Accumulate token usage
			result.TokensInput += event.TokensInput
			result.TokensOutput += event.TokensOutput
			if event.Model != "" {
				result.Model = event.Model
			}
		}
	}()

	// Read stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer logging.Recover("executor.qwencode.stderr")
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line + "\n")
			if opts.Verbose {
				fmt.Printf("   [err] %s\n", line)
			}
		}
	}()

	// Monitor context for timeout and handle hard kill
	go func() {
		defer logging.Recover("executor.qwencode.context")
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
				b.log.Debug("Process exited gracefully after context cancellation",
					slog.Int("pid", cmd.Process.Pid),
				)
				return
			case <-time.After(GracePeriod):
				if cmd.Process != nil {
					b.log.Warn("Grace period expired, sending SIGKILL",
						slog.Int("pid", cmd.Process.Pid),
					)
					if err := cmd.Process.Kill(); err != nil {
						b.log.Error("Failed to kill process",
							slog.Int("pid", cmd.Process.Pid),
							slog.Any("error", err),
						)
					} else {
						b.log.Info("Process killed successfully",
							slog.Int("pid", cmd.Process.Pid),
						)
					}
				}
			}
		}
	}()

	// Wait for output readers
	wg.Wait()

	// Wait for command to complete
	err = cmd.Wait()
	close(cmdDone)

	// #27: collect RSS sample (cancelling the sampler triggers the final read).
	cancelRSSSampler()
	if rssSample, ok := <-rssCh; ok {
		result.PeakRSSMB = rssSample.PeakMB
		result.FinalRSSMB = rssSample.FinalMB
		if rssSample.PeakMB > 0 {
			b.log.Debug("Subprocess RSS telemetry",
				slog.Int("peak_rss_mb", rssSample.PeakMB),
				slog.Int("final_rss_mb", rssSample.FinalMB),
			)
		}
	}

	stderrStr := stderrOutput.String()
	// GH-2328/#25: surface stderr on every path so persistBackendDiagnostics can
	// write it to execution_logs, matching claude-code. Without this, qwen
	// failures were undiagnosable beyond the bare error string.
	result.Stderr = stderrStr

	if err != nil {
		result.Success = false

		qcErr := classifyQwenCodeError(stderrStr, err)
		// GH-2328/#25: carry the classification so GH-2328 diagnostics aren't empty.
		result.ErrorType = string(qcErr.Type)

		b.log.Warn("Qwen Code execution failed",
			slog.String("error_type", string(qcErr.Type)),
			slog.String("message", qcErr.Message),
			slog.String("stderr", qcErr.Stderr),
		)

		// Fallback if --resume fails with session not found
		if qcErr.Type == QwenErrorTypeSessionNotFound && opts.ResumeSessionID != "" {
			b.log.Warn("qwen-code: session not found, retrying without --resume",
				"session_id", opts.ResumeSessionID)
			opts.ResumeSessionID = ""
			return b.Execute(ctx, opts)
		}

		if result.Error == "" {
			result.Error = qcErr.Error()
		}

		return result, qcErr
	}

	result.Success = true
	return result, nil
}

package executor

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// executeWithFromPR is the internal implementation that allows controlling --from-pr usage.
// When allowFromPR is false, it skips --from-pr even if opts.FromPR is set.
// This enables fallback retry without --from-pr if the session is not found.
func (b *ClaudeCodeBackend) executeWithFromPR(ctx context.Context, opts ExecuteOptions, allowFromPR bool) (*BackendResult, error) {
	// Build command arguments
	var args []string

	// GH-1267: Use --from-pr for session resumption from PR context
	// This takes precedence over --resume since it's more specific.
	if opts.FromPR > 0 && allowFromPR && b.config.UseFromPR {
		args = []string{
			"--from-pr", strconv.Itoa(opts.FromPR),
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		b.log.Info("Resuming session from PR context",
			slog.Int("pr", opts.FromPR),
		)
	} else if opts.ResumeSessionID != "" {
		// GH-1265: Use --resume for session continuation (e.g., self-review)
		args = []string{
			"--resume", opts.ResumeSessionID,
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
		b.log.Info("Resuming session for context continuation",
			slog.String("session_id", opts.ResumeSessionID),
		)
	} else {
		args = []string{
			"-p", opts.Prompt,
			"--verbose",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
		}
	}

	// Add model flag if specified (model routing GH-215)
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
		b.log.Info("Using routed model", slog.String("model", opts.Model))
	}

	// Add max-turns flag if set by .pilot/workflow.yaml agent.max_turns (TASK-304)
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(opts.MaxTurns))
		b.log.Info("Using workflow max_turns override", slog.Int("max_turns", opts.MaxTurns))
	}

	// Add effort flag if specified (effort routing)
	// Note: Claude Code CLI may not support --effort yet; this is future-proofed.
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
		b.log.Info("Using routed effort", slog.String("effort", opts.Effort))
	}

	// GH-2432: --allowedTools restricts the subprocess toolbox; --mcp-config
	// scopes which MCP servers (if any) the subprocess loads. Both cut the
	// per-turn token cost considerably when MCPs are not needed.
	if len(opts.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(opts.AllowedTools, ","))
	}
	if opts.MCPConfigPath != "" {
		args = append(args, "--mcp-config", opts.MCPConfigPath)
	}

	args = append(args, b.config.ExtraArgs...)

	cmd := exec.CommandContext(ctx, b.config.Command, args...)
	cmd.Dir = opts.ProjectPath

	// GH-2328: Signal executor mode to the child process. Project `CLAUDE.md`
	// and auto-memory can detect this and skip Navigator-only "DO NOT write
	// code" rules without relying on prompt-prefix heuristics.
	env := append(os.Environ(), "PILOT_EXECUTOR=1")
	// GH-2371: route the CC subprocess to the configured provider when set.
	// Values are appended after os.Environ() so they win on Node's last-write
	// lookup if the user's shell also exports ANTHROPIC_*.
	if b.apiBaseURL != "" {
		env = append(env, "ANTHROPIC_BASE_URL="+b.apiBaseURL)
	}
	if b.apiAuthToken != "" {
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+b.apiAuthToken)
	}
	if b.defaultModel != "" {
		env = append(env, "ANTHROPIC_MODEL="+b.defaultModel)
	}
	// Pass context window and output token env vars if configured (GH-2163).
	if b.config.Disable1MContext {
		env = append(env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
	}
	if b.config.MaxOutputTokens > 0 {
		env = append(env, fmt.Sprintf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=%d", b.config.MaxOutputTokens))
	}
	cmd.Env = env

	b.log.Debug("Starting Claude Code",
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
		return nil, fmt.Errorf("failed to start Claude Code: %w", err)
	}
	b.log.Debug("Claude Code started", slog.Int("pid", cmd.Process.Pid))

	// GH-3028: apply RSS cap (Linux: RLIMIT_AS via prlimit64; darwin/other: no-op).
	applyResourceLimits(cmd.Process.Pid, b.subprocessLimits)

	// GH-3028: start RSS sampler — collects peak/final RSS for telemetry.
	sampleInterval := 10 * time.Second
	if b.subprocessLimits != nil && b.subprocessLimits.SampleIntervalSec > 0 {
		sampleInterval = time.Duration(b.subprocessLimits.SampleIntervalSec) * time.Second
	}
	rssSamplerCtx, cancelRSSSampler := context.WithCancel(context.Background())
	rssCh := StartRSSSampler(rssSamplerCtx, cmd.Process.Pid, sampleInterval)

	// Track results
	result := &BackendResult{}
	// GH-2332: bounded stderr buffer to prevent OOM on long sessions.
	stderrOutput := newBoundedBuffer(MaxStderrBufferBytes)
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

					// Invoke callback if provided
					if opts.HeartbeatCallback != nil {
						opts.HeartbeatCallback(cmd.Process.Pid, age)
					}

					// Kill the hung process
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

	// Watchdog goroutine: hard kill after absolute timeout (GH-882)
	// This is a safety net for processes that ignore context cancellation.
	if opts.WatchdogTimeout > 0 {
		go func() {
			select {
			case <-cmdDone:
				// Command completed normally, watchdog not needed
				return
			case <-time.After(opts.WatchdogTimeout):
				// Watchdog timeout expired, forcibly kill the process
				if cmd.Process == nil {
					return
				}

				b.log.Warn("Watchdog timeout expired, forcibly killing subprocess",
					slog.Int("pid", cmd.Process.Pid),
					slog.Duration("watchdog_timeout", opts.WatchdogTimeout),
				)

				// Invoke callback before killing (allows alert emission)
				if opts.WatchdogCallback != nil {
					opts.WatchdogCallback(cmd.Process.Pid, opts.WatchdogTimeout)
				}

				// Kill the process
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
		scanner := bufio.NewScanner(stdout)
		// Increase buffer size for large JSON events
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()

			// Update heartbeat timestamp on each stream event
			lastEventAt.Store(time.Now().UnixNano())

			if opts.Verbose {
				fmt.Printf("   %s\n", line)
			}

			// Parse and convert to BackendEvent
			event := b.parseStreamEvent(line)
			if opts.EventHandler != nil {
				opts.EventHandler(event)
			}

			// GH-2328: track the last assistant text block so refusals (Claude
			// exits 0 after politely declining) can be surfaced to the user.
			if event.Type == EventTypeText && event.Message != "" {
				result.LastAssistantText = event.Message
			}

			// Track final result
			if event.Type == EventTypeResult {
				// GH-2103: Cancel heartbeat on result event.
				// On slow I/O flush, the heartbeat timer could fire and kill
				// the process after it had already produced output.
				cancelHeartbeat()

				if event.IsError {
					result.Error = event.Message
				} else {
					result.Output = event.Message
					result.SawSuccessResult = true // GH-2107: track successful result for timeout recovery
				}
				// Cancel heartbeat — process is finishing, don't kill it
				cancelHeartbeat()
			}

			// Capture session ID from init event (GH-1265)
			if event.Type == EventTypeInit && event.SessionID != "" {
				result.SessionID = event.SessionID
			}

			// Accumulate token usage
			result.TokensInput += event.TokensInput
			result.TokensOutput += event.TokensOutput
			result.CacheCreationInputTokens += event.CacheCreationInputTokens
			result.CacheReadInputTokens += event.CacheReadInputTokens
			if event.Model != "" {
				result.Model = event.Model
			}
		}
	}()

	// Read stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteLine(line)
			if opts.Verbose {
				fmt.Printf("   [err] %s\n", line)
			}
		}
	}()

	// Monitor context for timeout and handle hard kill
	go func() {
		select {
		case <-cmdDone:
			// Command completed normally, nothing to do
			return
		case <-ctx.Done():
			// Context cancelled (timeout or explicit cancellation)
			// exec.CommandContext will send SIGTERM/interrupt, wait grace period then SIGKILL
			if cmd.Process == nil {
				return
			}

			b.log.Warn("Context cancelled, waiting grace period before hard kill",
				slog.Int("pid", cmd.Process.Pid),
				slog.Duration("grace_period", GracePeriod),
			)

			// Wait for grace period or command to exit
			select {
			case <-cmdDone:
				// Process exited gracefully after signal
				b.log.Debug("Process exited gracefully after context cancellation",
					slog.Int("pid", cmd.Process.Pid),
				)
				return
			case <-time.After(GracePeriod):
				// Grace period expired, hard kill
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
	close(cmdDone) // Signal that command is done

	// GH-3028: collect RSS sample (cancelling the sampler goroutine triggers final read).
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

	if err != nil {
		// GH-2107: If a successful result event was seen before the process exited with
		// an error, the work was completed but Claude Code timed out on a subsequent turn
		// (e.g., writing final summary). Recover as success.
		if result.SawSuccessResult {
			b.log.Info("Recovering success: process exited with error after successful result event (GH-2107)",
				slog.String("exit_error", err.Error()),
				slog.String("output_preview", truncate(result.Output, 200)),
			)
			result.Success = true
			return result, nil
		}

		result.Success = false

		// GH-917: Classify the error for better handling
		stderr := stderrOutput.String()
		ccErr := parseClaudeCodeError(stderr, err).(*ClaudeCodeError)

		// GH-2328: surface the raw stderr + classification so the runner can
		// write them to execution_logs. Without this, "unknown: exit status 1"
		// is all the user ever sees and diagnosis requires re-running with a
		// patched binary.
		result.Stderr = stderr
		result.ErrorType = string(ccErr.Type)

		// GH-2112: Log OOM kills at error level for monitoring
		if ccErr.Type == ErrorTypeOOM {
			b.log.Error("Claude Code process OOM-killed",
				slog.String("error_type", string(ccErr.Type)),
				slog.String("message", ccErr.Message),
				slog.String("stderr", ccErr.Stderr),
			)
		} else {
			b.log.Warn("Claude Code execution failed",
				slog.String("error_type", string(ccErr.Type)),
				slog.String("message", ccErr.Message),
				slog.String("stderr", ccErr.Stderr),
			)
		}

		// Store classified error info in result
		if result.Error == "" {
			result.Error = ccErr.Error()
		}

		// Return classified error for upstream handling
		return result, ccErr
	}

	result.Success = true
	// GH-2328: expose stderr on success too so warnings (e.g. rate-limit
	// overage rejected, context window warnings) can be logged.
	result.Stderr = stderrOutput.String()
	return result, nil
}

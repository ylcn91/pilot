package executor

import "log/slog"

// finalizeResult applies post-Wait success/error classification to a run result.
// waitErr is the error returned by cmd.Wait; stderr is the captured stderr output.
func (b *ClaudeCodeBackend) finalizeResult(result *BackendResult, waitErr error, stderr string) (*BackendResult, error) {
	if waitErr != nil {
		// GH-2107: If a successful result event was seen before the process exited with
		// an error, the work was completed but Claude Code timed out on a subsequent turn
		// (e.g., writing final summary). Recover as success.
		if result.SawSuccessResult {
			b.log.Info("Recovering success: process exited with error after successful result event (GH-2107)",
				slog.String("exit_error", waitErr.Error()),
				slog.String("output_preview", truncate(result.Output, 200)),
			)
			result.Success = true
			return result, nil
		}

		result.Success = false

		// GH-917: Classify the error for better handling
		ccErr := parseClaudeCodeError(stderr, waitErr).(*ClaudeCodeError)

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
	result.Stderr = stderr
	return result, nil
}

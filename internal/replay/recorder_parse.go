package replay

import (
	"encoding/json"
	"strings"
	"time"
)

// parseEvent extracts structured data from a raw stream event
func (r *Recorder) parseEvent(rawJSON string) *ParsedEvent {
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return nil
	}

	parsed := &ParsedEvent{}

	// Get event type
	if t, ok := raw["type"].(string); ok {
		parsed.Type = t
	}
	if st, ok := raw["subtype"].(string); ok {
		parsed.Subtype = st
	}

	// Handle result events
	if parsed.Type == "result" {
		if result, ok := raw["result"].(string); ok {
			parsed.Result = result
		}
		if isErr, ok := raw["is_error"].(bool); ok {
			parsed.IsError = isErr
		}
		// Extract usage from result
		if usage, ok := raw["usage"].(map[string]any); ok {
			if in, ok := usage["input_tokens"].(float64); ok {
				parsed.InputTokens = int64(in)
			}
			if out, ok := usage["output_tokens"].(float64); ok {
				parsed.OutputTokens = int64(out)
			}
		}
	}

	// Handle assistant events (tool calls, text)
	if parsed.Type == "assistant" {
		if msg, ok := raw["message"].(map[string]any); ok {
			if content, ok := msg["content"].([]any); ok {
				var textParts []string
				for _, block := range content {
					if b, ok := block.(map[string]any); ok {
						blockType, _ := b["type"].(string)
						switch blockType {
						case "tool_use":
							// Keep the first tool_use; do not let a later block clobber it.
							if parsed.ToolName == "" {
								parsed.ToolName, _ = b["name"].(string)
								if input, ok := b["input"].(map[string]any); ok {
									parsed.ToolInput = input
									// Extract file path for file operations
									if fp, ok := input["file_path"].(string); ok {
										parsed.FilePath = fp
										parsed.FileOperation = r.detectFileOp(parsed.ToolName)
									}
								}
							}
						case "text":
							// Accumulate text across blocks so a turn with both a
							// text block and a tool_use block preserves both.
							if text, ok := b["text"].(string); ok && text != "" {
								textParts = append(textParts, text)
							}
						}
					}
				}
				parsed.Text = strings.Join(textParts, "\n")
			}
		}
	}

	return parsed
}

// detectFileOp determines the file operation from tool name
func (r *Recorder) detectFileOp(toolName string) string {
	switch toolName {
	case "Read":
		return "read"
	case "Write":
		return "create"
	case "Edit":
		return "modify"
	default:
		return ""
	}
}

// detectPhase determines execution phase from event
func (r *Recorder) detectPhase(parsed *ParsedEvent) string {
	// From tool usage
	switch parsed.ToolName {
	case "Read", "Glob", "Grep":
		return "Exploring"
	case "Write", "Edit":
		return "Implementing"
	case "Bash":
		if cmd, ok := parsed.ToolInput["command"].(string); ok {
			cmdLower := strings.ToLower(cmd)
			if strings.Contains(cmdLower, "git commit") {
				return "Committing"
			}
			if strings.Contains(cmdLower, "test") {
				return "Testing"
			}
		}
		return ""
	}

	// From text patterns (Navigator phases)
	if parsed.Text != "" {
		text := parsed.Text
		if strings.Contains(text, "PHASE:") || strings.Contains(text, "Phase:") {
			if strings.Contains(text, "RESEARCH") || strings.Contains(text, "Research") {
				return "Research"
			}
			if strings.Contains(text, "IMPL") || strings.Contains(text, "Implement") {
				return "Implementing"
			}
			if strings.Contains(text, "VERIFY") || strings.Contains(text, "Verify") {
				return "Verifying"
			}
			if strings.Contains(text, "COMPLETE") || strings.Contains(text, "Complete") {
				return "Completing"
			}
		}
	}

	return ""
}

// recordPhaseEnd records the end of a phase timing
func (r *Recorder) recordPhaseEnd() {
	if r.currentPhase != "" && !r.phaseStart.IsZero() {
		timing := PhaseTiming{
			Phase:    r.currentPhase,
			Start:    r.phaseStart,
			End:      time.Now(),
			Duration: time.Since(r.phaseStart),
		}
		r.recording.PhaseTimings = append(r.recording.PhaseTimings, timing)
	}
}

// trackFileChange tracks file modifications
func (r *Recorder) trackFileChange(parsed *ParsedEvent) {
	// For now, just track that the file was touched
	// Full diff tracking would require before/after content
	if _, exists := r.diffFiles[parsed.FilePath]; !exists {
		r.diffFiles[parsed.FilePath] = &FileDiff{
			Timestamp: time.Now(),
			FilePath:  parsed.FilePath,
			Operation: parsed.FileOperation,
		}
	}
}

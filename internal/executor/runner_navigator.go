package executor

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// parseNavigatorPatterns detects Navigator-specific progress signals from text
func (r *Runner) parseNavigatorPatterns(taskID, text string, state *progressState) {
	// Try structured signal parser v2 first (GH-960)
	if r.signalParser != nil {
		signals := r.signalParser.ParseSignals(text)
		if len(signals) > 0 {
			r.handleStructuredSignals(taskID, signals, state)
			return
		}
	}

	// Fall back to legacy string-based parsing for backward compatibility
	// Navigator Session Started
	if strings.Contains(text, "Navigator Session Started") {
		state.hasNavigator = true
		r.reportProgress(taskID, "Navigator", 10, "Navigator session started")
		return
	}

	// Navigator Status Block - extract phase and progress
	if strings.Contains(text, "NAVIGATOR_STATUS") {
		state.hasNavigator = true
		r.parseNavigatorStatusBlock(taskID, text, state)
		return
	}

	// Phase transitions
	if strings.Contains(text, "PHASE:") && strings.Contains(text, "→") {
		// Extract phase from "PHASE: X → Y" pattern
		if idx := strings.Index(text, "→"); idx != -1 {
			after := strings.TrimSpace(text[idx+3:]) // Skip "→ "
			if newline := strings.Index(after, "\n"); newline != -1 {
				after = after[:newline]
			}
			phase := strings.TrimSpace(after)
			if phase != "" {
				r.handleNavigatorPhase(taskID, phase, state)
			}
		}
		return
	}

	// Workflow check - indicates task analysis
	if strings.Contains(text, "WORKFLOW CHECK") {
		if state.phase != "Analyzing" {
			state.phase = "Analyzing"
			r.reportProgress(taskID, "Analyzing", 12, "Workflow check...")
		}
		return
	}

	// Task Mode
	if strings.Contains(text, "TASK MODE ACTIVATED") {
		r.reportProgress(taskID, "Task Mode", 15, "Task mode activated")
		return
	}

	// Completion signals
	if strings.Contains(text, "LOOP COMPLETE") || strings.Contains(text, "TASK MODE COMPLETE") {
		state.exitSignal = true
		r.reportProgress(taskID, "Completing", 95, "Task complete signal received")
		return
	}

	// EXIT_SIGNAL detection
	if strings.Contains(text, "EXIT_SIGNAL: true") || strings.Contains(text, "EXIT_SIGNAL:true") {
		state.exitSignal = true
		r.reportProgress(taskID, "Finishing", 92, "Exit signal detected")
		return
	}

	// Stagnation detection
	if strings.Contains(text, "STAGNATION DETECTED") {
		r.reportProgress(taskID, "⚠️ Stalled", 0, "Navigator detected stagnation")
		return
	}
}

// parseNavigatorStatusBlock extracts progress from Navigator status block
func (r *Runner) parseNavigatorStatusBlock(taskID, text string, state *progressState) {
	// Extract Phase: from status block
	if idx := strings.Index(text, "Phase:"); idx != -1 {
		line := text[idx:]
		if newline := strings.Index(line, "\n"); newline != -1 {
			line = line[:newline]
		}
		phase := strings.TrimSpace(strings.TrimPrefix(line, "Phase:"))
		if phase != "" {
			r.handleNavigatorPhase(taskID, phase, state)
		}
	}

	// Extract Progress: percentage
	if idx := strings.Index(text, "Progress:"); idx != -1 {
		line := text[idx:]
		if newline := strings.Index(line, "\n"); newline != -1 {
			line = line[:newline]
		}
		// Parse "Progress: 45%" or similar
		line = strings.TrimPrefix(line, "Progress:")
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, "%")
		if pct, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			// Clamp progress to valid range (GH-941)
			if pct < 0 {
				r.log.Warn("Legacy parser: clamping negative progress",
					slog.String("task_id", taskID),
					slog.Int("original", pct),
				)
				pct = 0
			}
			if pct > 100 {
				r.log.Warn("Legacy parser: clamping progress > 100",
					slog.String("task_id", taskID),
					slog.Int("original", pct),
				)
				pct = 100
			}
			state.navProgress = pct
		}
	}

	// Extract Iteration
	if idx := strings.Index(text, "Iteration:"); idx != -1 {
		line := text[idx:]
		if newline := strings.Index(line, "\n"); newline != -1 {
			line = line[:newline]
		}
		// Parse "Iteration: 2/5" format
		line = strings.TrimPrefix(line, "Iteration:")
		if slash := strings.Index(line, "/"); slash != -1 {
			if iter, err := strconv.Atoi(strings.TrimSpace(line[:slash])); err == nil {
				state.navIteration = iter
			}
		}
	}
}

// handleStructuredSignals processes v2 structured pilot signals (GH-960)
func (r *Runner) handleStructuredSignals(taskID string, signals []PilotSignal, state *progressState) {
	if len(signals) == 0 {
		return
	}

	// Mark as having Navigator
	state.hasNavigator = true

	// Process signals in order
	for _, signal := range signals {
		r.log.Debug("Processing structured signal",
			slog.String("task_id", taskID),
			slog.String("type", signal.Type),
			slog.String("phase", signal.Phase),
			slog.Int("progress", signal.Progress),
		)

		switch signal.Type {
		case SignalTypeStatus:
			// Update phase if provided
			if signal.Phase != "" {
				r.handleNavigatorPhase(taskID, signal.Phase, state)
			}
			// Update progress if provided
			if signal.Progress > 0 {
				state.navProgress = signal.Progress
			}
			// Update iteration if provided
			if signal.Iteration > 0 {
				state.navIteration = signal.Iteration
			}

		case SignalTypePhase:
			if signal.Phase != "" {
				r.handleNavigatorPhase(taskID, signal.Phase, state)
			}

		case SignalTypeExit:
			state.exitSignal = true
			r.reportProgress(taskID, "Finishing", 95, signal.Message)

		case SignalTypeStagnation:
			r.reportProgress(taskID, "⚠️ Stalled", 0, "Navigator detected stagnation")
		}

		// Check for exit signal from any signal type
		if signal.ExitSignal {
			state.exitSignal = true
			message := signal.Message
			if message == "" {
				message = "Exit signal detected"
			}
			r.reportProgress(taskID, "Finishing", 92, message)
		}
	}
}

// handleNavigatorPhase maps Navigator phases to progress
func (r *Runner) handleNavigatorPhase(taskID, phase string, state *progressState) {
	phase = strings.ToUpper(strings.TrimSpace(phase))

	// Skip if same phase
	if state.navPhase == phase {
		return
	}
	state.navPhase = phase

	var displayPhase string
	var progress int
	var message string

	switch phase {
	case "INIT":
		displayPhase = "Init"
		progress = 10
		message = "Initializing task..."
	case "RESEARCH":
		displayPhase = "Research"
		progress = 25
		message = "Researching codebase..."
	case "IMPL", "IMPLEMENTATION":
		displayPhase = "Implement"
		progress = 50
		message = "Implementing changes..."
	case "VERIFY", "VERIFICATION":
		displayPhase = "Verify"
		progress = 80
		message = "Verifying changes..."
	case "COMPLETE", "COMPLETED":
		displayPhase = "Complete"
		progress = 95
		message = "Finalizing..."
	default:
		displayPhase = phase
		progress = 50
		message = fmt.Sprintf("Phase: %s", phase)
	}

	// Use Navigator's reported progress if available
	if state.navProgress > 0 {
		progress = state.navProgress
	}

	state.phase = displayPhase
	r.reportProgress(taskID, displayPhase, progress, message)
}

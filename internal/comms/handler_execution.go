package comms

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/intent"
)

// ---------- direct task API ----------

// DirectTaskOpts provides options for direct task execution via ExecuteDirectTask.
type DirectTaskOpts struct {
	ForcePR   *bool  // nil = auto-detect, true = force PR, false = no PR
	ImagePath string // path to image file for image analysis tasks
}

// ExecuteDirectTask creates and executes a task directly, bypassing intent detection
// and the confirmation flow. Used by adapter command handlers (/run, /nopr, /pr, images).
func (h *Handler) ExecuteDirectTask(ctx context.Context, contextID, threadID, taskID, description string, opts *DirectTaskOpts) {
	createPR := h.shouldCreatePR(description)
	var imagePath string

	if opts != nil {
		if opts.ForcePR != nil {
			createPR = *opts.ForcePR
		}
		imagePath = opts.ImagePath
	}

	h.executeTaskCore(ctx, contextID, threadID, taskID, description, createPR, imagePath)
}

func (h *Handler) shouldCreatePR(description string) bool {
	detectEphemeral := true
	if h.runner.Config() != nil && h.runner.Config().DetectEphemeral != nil {
		detectEphemeral = *h.runner.Config().DetectEphemeral
	}
	if detectEphemeral && intent.IsEphemeralTask(description) {
		return false
	}
	return true
}

// ---------- confirmation & execution ----------

func (h *Handler) handleConfirmation(ctx context.Context, contextID, threadID string, confirmed bool) {
	h.mu.Lock()
	pending, exists := h.pendingTasks[contextID]
	if exists {
		delete(h.pendingTasks, contextID)
	}
	h.mu.Unlock()

	if !exists {
		_ = h.messenger.SendText(ctx, contextID, "No pending task to confirm.")
		return
	}

	if !confirmed {
		_ = h.messenger.SendText(ctx, contextID, fmt.Sprintf("❌ Task %s cancelled.", pending.TaskID))
		return
	}

	h.executeTask(ctx, contextID, threadID, pending.TaskID, pending.Description)
}

func (h *Handler) executeTask(ctx context.Context, contextID, threadID, taskID, description string) {
	createPR := h.shouldCreatePR(description)
	h.executeTaskCore(ctx, contextID, threadID, taskID, description, createPR, "")
}

func (h *Handler) executeTaskCore(ctx context.Context, contextID, threadID, taskID, description string, createPR bool, imagePath string) {
	// Send starting message
	prNote := ""
	if !createPR {
		prNote = " (no PR)"
	}
	detail := fmt.Sprintf("🚀 Starting %s%s...", taskID, prNote)
	msgRef, err := h.messenger.SendProgress(ctx, contextID, "", taskID, "Starting"+prNote, 0, "Initializing...")
	if err != nil {
		h.log.Warn("Failed to send progress start", slog.Any("error", err))
		_ = h.messenger.SendText(ctx, contextID, detail)
	}

	// Track running task
	taskCtx, taskCancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.runningTasks[contextID] = &RunningTask{
		TaskID:    taskID,
		ContextID: contextID,
		StartedAt: time.Now(),
		Cancel:    taskCancel,
	}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.runningTasks, contextID)
		h.mu.Unlock()
		taskCancel()
	}()

	// Build executor task
	branch := ""
	baseBranch := ""
	if createPR {
		branch = fmt.Sprintf("pilot/%s", taskID)
		baseBranch = "main"
	}

	memberID := h.resolveMemberID(contextID)

	task := &executor.Task{
		ID:          taskID,
		Title:       TruncateText(description, 50),
		Description: description,
		ProjectPath: h.getActiveProjectPath(contextID),
		Verbose:     false,
		Branch:      branch,
		BaseBranch:  baseBranch,
		CreatePR:    createPR,
		MemberID:    memberID,
		ImagePath:   imagePath,
	}

	// Progress callback with throttling (named callback for parallel-safe execution)
	callbackName := fmt.Sprintf("comms-%s", taskID)
	if msgRef != "" && h.runner != nil {
		var lastPhase string
		var lastProgress int
		var lastUpdate time.Time

		h.runner.AddProgressCallback(callbackName, func(tid, phase string, progress int, message string) {
			if tid != taskID {
				return
			}
			now := time.Now()
			phaseChanged := phase != lastPhase
			progressChanged := progress-lastProgress >= 15
			timeElapsed := now.Sub(lastUpdate) >= 3*time.Second
			if !phaseChanged && !progressChanged && !timeElapsed {
				return
			}
			lastPhase = phase
			lastProgress = progress
			lastUpdate = now

			newRef, _ := h.messenger.SendProgress(ctx, contextID, msgRef, taskID, phase, progress, message)
			if newRef != "" {
				msgRef = newRef
			}
		})
	}

	// Execute
	h.log.Info("Executing task",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID))
	result, err := h.runner.Execute(taskCtx, task)

	// Remove named progress callback
	if h.runner != nil {
		h.runner.RemoveProgressCallback(callbackName)
	}

	if err != nil {
		_ = h.messenger.SendResult(ctx, contextID, threadID, taskID, false, err.Error(), "")
		return
	}

	output := CleanInternalSignals(result.Output)
	_ = h.messenger.SendResult(ctx, contextID, threadID, taskID, result.Success, output, result.PRUrl)
}

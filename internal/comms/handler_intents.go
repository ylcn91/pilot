package comms

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/executor"
)

// ---------- intent handlers ----------

func (h *Handler) handleGreeting(ctx context.Context, contextID string) {
	_ = h.messenger.SendText(ctx, contextID, "👋 Hello! I'm Pilot — send me a task, question, or say /help.")
}

func (h *Handler) handleQuestion(ctx context.Context, contextID, threadID, question string) {
	_ = h.messenger.SendText(ctx, contextID, "🔍 Looking into that...")

	questionCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Answer this question about the codebase. DO NOT make any changes, only read and analyze.

Question: %s

IMPORTANT: Be concise. Limit your exploration to 5-10 files max. Provide a brief, direct answer.
If the question is too broad, ask for clarification instead of exploring everything.`, question)

	taskID := fmt.Sprintf("Q-%d", time.Now().Unix())
	task := &executor.Task{
		ID:          taskID,
		Title:       "Question: " + TruncateText(question, 40),
		Description: prompt,
		ProjectPath: h.getActiveProjectPath(contextID),
		Verbose:     false,
	}

	h.log.Debug("Answering question", slog.String("task_id", taskID), slog.String("context_id", contextID))
	result, err := h.runner.Execute(questionCtx, task)

	if err != nil {
		if questionCtx.Err() == context.DeadlineExceeded {
			_ = h.messenger.SendText(ctx, contextID, "⏱ Question timed out. Try asking something more specific.")
		} else {
			_ = h.messenger.SendText(ctx, contextID, "❌ Sorry, I couldn't answer that question. Try rephrasing it.")
		}
		return
	}

	answer := CleanInternalSignals(result.Output)
	if answer == "" {
		answer = "I couldn't find a clear answer to that question."
	}

	_ = h.messenger.SendChunked(ctx, contextID, threadID, answer, "")
}

func (h *Handler) handleResearch(ctx context.Context, contextID, threadID, query string) {
	_ = h.messenger.SendText(ctx, contextID, "🔬 Researching...")

	taskID := fmt.Sprintf("RES-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Research: " + TruncateText(query, 40),
		Description: fmt.Sprintf(`Research and analyze: %s

Provide findings in a structured format with:
- Executive summary
- Key findings
- Relevant code/files if applicable
- Recommendations

DO NOT make any code changes. This is a read-only research task.`, query),
		ProjectPath: h.getActiveProjectPath(contextID),
		CreatePR:    false,
	}

	researchCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	h.log.Info("Executing research", slog.String("task_id", taskID), slog.String("context_id", contextID))
	result, err := h.runner.Execute(researchCtx, task)

	if err != nil {
		if researchCtx.Err() == context.DeadlineExceeded {
			_ = h.messenger.SendText(ctx, contextID, "⏱ Research timed out. Try a more specific query.")
		} else {
			_ = h.messenger.SendText(ctx, contextID, fmt.Sprintf("❌ Research failed: %s", err.Error()))
		}
		return
	}

	content := CleanInternalSignals(result.Output)
	if content == "" {
		_ = h.messenger.SendText(ctx, contextID, "Research completed but produced no output.")
		return
	}

	_ = h.messenger.SendChunked(ctx, contextID, threadID, content, "")
}

func (h *Handler) handlePlanning(ctx context.Context, contextID, threadID, request string) {
	_ = h.messenger.SendText(ctx, contextID, "📐 Drafting plan...")

	taskID := fmt.Sprintf("PLAN-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Plan: " + TruncateText(request, 40),
		Description: fmt.Sprintf(`Create an implementation plan for: %s

Explore the codebase and propose a detailed plan. Include:
1. Summary of approach
2. Files to modify/create
3. Step-by-step implementation phases
4. Potential risks or considerations

DO NOT make any code changes. Only explore and plan.`, request),
		ProjectPath: h.getActiveProjectPath(contextID),
		CreatePR:    false,
	}

	planTimeout := 2 * time.Minute
	if h.runner.Config() != nil && h.runner.Config().PlanningTimeout > 0 {
		planTimeout = h.runner.Config().PlanningTimeout
	}
	planCtx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()

	h.log.Info("Creating plan", slog.String("task_id", taskID), slog.String("context_id", contextID))
	result, err := h.runner.Execute(planCtx, task)

	if err != nil {
		if planCtx.Err() == context.DeadlineExceeded {
			_ = h.messenger.SendText(ctx, contextID, "⏱ Planning timed out. Try a simpler request.")
		} else {
			_ = h.messenger.SendText(ctx, contextID, fmt.Sprintf("❌ Planning failed: %s", err.Error()))
		}
		return
	}

	planContent := CleanInternalSignals(result.Output)
	if planContent == "" {
		_ = h.messenger.SendText(ctx, contextID, "Planning completed but produced no output.")
		return
	}

	// Store plan as pending task for execution
	h.mu.Lock()
	h.pendingTasks[contextID] = &PendingTask{
		TaskID:      taskID,
		Description: fmt.Sprintf("## Implementation Plan\n\n%s\n\n## Original Request\n\n%s", planContent, request),
		ContextID:   contextID,
		ThreadID:    threadID,
		SenderID:    h.lastSender[contextID],
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	// Send plan with confirmation prompt
	summary := TruncateText(planContent, h.messenger.MaxMessageLength()-100)
	_, err = h.messenger.SendConfirmation(ctx, contextID, threadID, taskID, summary, h.getActiveProjectPath(contextID))
	if err != nil {
		h.log.Warn("Failed to send plan confirmation, falling back to text", slog.Any("error", err))
		_ = h.messenger.SendChunked(ctx, contextID, threadID, planContent, "📋 Implementation Plan")
		_ = h.messenger.SendText(ctx, contextID, "Reply yes to execute or no to cancel.")
	}
}

func (h *Handler) handleChat(ctx context.Context, contextID, threadID, message string) {
	_ = h.messenger.SendText(ctx, contextID, "💬 Thinking...")

	taskID := fmt.Sprintf("CHAT-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Chat: " + TruncateText(message, 30),
		Description: fmt.Sprintf(`You are Pilot, an AI assistant for the codebase at %s.

The user wants to have a conversation (not execute a task).
Respond helpfully and conversationally. You can reference project knowledge but DO NOT make code changes.

Be concise - this is a chat conversation, not a report. Keep response under 500 words.

User message: %s`, h.getActiveProjectPath(contextID), message),
		ProjectPath: h.getActiveProjectPath(contextID),
		CreatePR:    false,
	}

	chatCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	h.log.Debug("Chat response", slog.String("task_id", taskID), slog.String("context_id", contextID))
	result, err := h.runner.Execute(chatCtx, task)

	if err != nil {
		if chatCtx.Err() == context.DeadlineExceeded {
			_ = h.messenger.SendText(ctx, contextID, "⏱ Took too long to respond. Try a simpler question.")
		} else {
			_ = h.messenger.SendText(ctx, contextID, "Sorry, I couldn't process that. Try rephrasing?")
		}
		return
	}

	response := CleanInternalSignals(result.Output)
	if response == "" {
		response = "I'm not sure how to respond to that. Could you rephrase?"
	}

	// Truncate to fit platform limit
	maxLen := h.messenger.MaxMessageLength()
	if maxLen > 0 && len(response) > maxLen {
		response = response[:maxLen-3] + "..."
	}

	_ = h.messenger.SendText(ctx, contextID, response)

	// Record in conversation history
	if h.convStore != nil {
		h.convStore.Add(contextID, "assistant", TruncateText(response, 500))
	}
}

func (h *Handler) handleTask(ctx context.Context, contextID, threadID, description, senderID string) {
	// Task rate limit
	if !h.rateLimit.AllowTask(contextID) {
		h.log.Warn("Task rate limit exceeded", slog.String("context_id", contextID))
		_ = h.messenger.SendText(ctx, contextID,
			"⚠️ Task rate limit exceeded. You've submitted too many tasks recently. Please wait before submitting more.")
		return
	}

	// Check for existing pending task
	h.mu.Lock()
	if existing, exists := h.pendingTasks[contextID]; exists {
		h.mu.Unlock()
		_ = h.messenger.SendText(ctx, contextID,
			fmt.Sprintf("⚠️ You already have a pending task: %s\n\nReply yes to execute or no to cancel.", existing.TaskID))
		return
	}
	h.mu.Unlock()

	taskID := fmt.Sprintf("%s-%d", h.taskIDPrefix, time.Now().Unix())

	h.mu.Lock()
	h.pendingTasks[contextID] = &PendingTask{
		TaskID:      taskID,
		Description: description,
		ContextID:   contextID,
		ThreadID:    threadID,
		SenderID:    senderID,
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	_, err := h.messenger.SendConfirmation(ctx, contextID, threadID, taskID, description, h.getActiveProjectPath(contextID))
	if err != nil {
		h.log.Warn("Failed to send task confirmation", slog.Any("error", err))
		_ = h.messenger.SendText(ctx, contextID,
			fmt.Sprintf("📋 Task %s\n\n%s\n\nReply yes to execute or no to cancel.",
				taskID, TruncateText(description, 500)))
	}
}

package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/logging"
)

// processUpdate handles a single update
func (h *Handler) processUpdate(ctx context.Context, update *Update) {
	// Handle callback queries (button clicks)
	if update.CallbackQuery != nil {
		h.handleCallback(ctx, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	msg := update.Message
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Handle photo messages
	if len(msg.Photo) > 0 {
		h.handlePhoto(ctx, chatID, msg)
		return
	}

	// Handle voice messages
	if msg.Voice != nil {
		h.handleVoice(ctx, chatID, msg)
		return
	}

	// Skip if no text
	if msg.Text == "" {
		return
	}

	// Security check: only process messages from allowed users/chats
	if len(h.allowedIDs) > 0 {
		senderID := int64(0)
		if msg.From != nil {
			senderID = msg.From.ID
		}

		if !h.allowedIDs[msg.Chat.ID] && !h.allowedIDs[senderID] {
			logging.WithComponent("telegram").Debug("Ignoring message from unauthorized chat/user",
				slog.Int64("chat_id", msg.Chat.ID), slog.Int64("sender_id", senderID))
			return
		}
	}

	text := strings.TrimSpace(msg.Text)
	text = stripBotMention(text, h.botUsername)

	// Commands stay local
	if strings.HasPrefix(text, "/") {
		h.handleCommand(ctx, chatID, text)
		return
	}

	// Delegate all other text to shared comms.Handler
	if h.commsHandler != nil {
		senderID := ""
		senderName := ""
		if msg.From != nil {
			senderID = strconv.FormatInt(msg.From.ID, 10)
			if msg.From.FirstName != "" {
				senderName = msg.From.FirstName
			}
		}
		h.commsHandler.HandleMessage(ctx, &comms.IncomingMessage{
			ContextID:  chatID,
			SenderID:   senderID,
			SenderName: senderName,
			Text:       text,
			Platform:   "telegram",
			Timestamp:  time.Now(),
		})
	}
}

// handleCallback processes callback queries from inline keyboards
func (h *Handler) handleCallback(ctx context.Context, callback *CallbackQuery) {
	if callback.Message == nil {
		return
	}

	chatID := strconv.FormatInt(callback.Message.Chat.ID, 10)
	data := callback.Data

	// Answer callback to remove loading state
	_ = h.client.AnswerCallback(ctx, callback.ID, "")

	switch {
	case data == "execute":
		if h.commsHandler != nil {
			senderID := ""
			if callback.From != nil {
				senderID = strconv.FormatInt(callback.From.ID, 10)
			}
			h.commsHandler.HandleMessage(ctx, &comms.IncomingMessage{
				ContextID:  chatID,
				SenderID:   senderID,
				Platform:   "telegram",
				IsCallback: true,
				CallbackID: callback.ID,
				ActionID:   "execute",
			})
		}
	case data == "cancel":
		if h.commsHandler != nil {
			senderID := ""
			if callback.From != nil {
				senderID = strconv.FormatInt(callback.From.ID, 10)
			}
			h.commsHandler.HandleMessage(ctx, &comms.IncomingMessage{
				ContextID:  chatID,
				SenderID:   senderID,
				Platform:   "telegram",
				IsCallback: true,
				CallbackID: callback.ID,
				ActionID:   "cancel",
			})
		}
	case strings.HasPrefix(data, "switch_"):
		projectName := strings.TrimPrefix(data, "switch_")
		h.cmdHandler.HandleCallbackSwitch(ctx, chatID, projectName)
	case data == "voice_check_status":
		h.sendVoiceSetupPrompt(ctx, chatID)
	case strings.HasPrefix(data, "approve:") || strings.HasPrefix(data, "reject:"):
		if h.approvalHandler != nil {
			userID := ""
			username := ""
			if callback.From != nil {
				userID = strconv.FormatInt(callback.From.ID, 10)
				username = callback.From.Username
				if username == "" {
					username = callback.From.FirstName
				}
			}
			h.approvalHandler.HandleCallback(ctx, callback.ID, data, userID, username)
		}
	}
}

// handleCommand processes bot commands
func (h *Handler) handleCommand(ctx context.Context, chatID, text string) {
	// Delegate to command handler
	h.cmdHandler.HandleCommand(ctx, chatID, text)
}

// handleRunCommand executes a task directly without confirmation
func (h *Handler) handleRunCommand(ctx context.Context, chatID, taskIDInput string) {
	// Check if already running a task
	if h.commsHandler != nil {
		if running := h.commsHandler.GetRunningTask(chatID); running != nil {
			elapsed := time.Since(running.StartedAt).Round(time.Second)
			_, _ = h.client.SendMessage(ctx, chatID,
				fmt.Sprintf("⚠️ Already running %s (%s)\n\nUse /stop to cancel it first.", running.TaskID, elapsed), "")
			return
		}
	}

	// Resolve task ID
	taskInfo := h.resolveTaskID(taskIDInput)
	if taskInfo == nil {
		_, _ = h.client.SendMessage(ctx, chatID,
			fmt.Sprintf("❌ Task %s not found\n\nUse /tasks to see available tasks.", taskIDInput), "")
		return
	}

	// Load task description
	description := h.loadTaskDescription(taskInfo)
	if description == "" {
		_, _ = h.client.SendMessage(ctx, chatID,
			fmt.Sprintf("❌ Could not load task %s", taskInfo.FullID), "")
		return
	}

	// Notify user
	_, _ = h.client.SendMessage(ctx, chatID,
		fmt.Sprintf("🚀 Starting task\n\n%s: %s", taskInfo.FullID, taskInfo.Title), "")

	// Execute directly via commsHandler
	if h.commsHandler != nil {
		h.commsHandler.ExecuteDirectTask(ctx, chatID, "", taskInfo.FullID, fmt.Sprintf("## Task: %s\n\n%s", taskInfo.FullID, description), nil)
	}
}

// stripBotMention removes a leading @username mention from message text (GH-2129).
func stripBotMention(text, botUsername string) string {
	if botUsername == "" {
		return text
	}
	prefix := "@" + botUsername
	if len(text) >= len(prefix) && strings.EqualFold(text[:len(prefix)], prefix) {
		text = strings.TrimSpace(text[len(prefix):])
	}
	return text
}

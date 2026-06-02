package approval

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ylcn91/pilot/internal/memory"
)

// SendApprovalRequest sends an approval request via Telegram
func (h *TelegramHandler) SendApprovalRequest(ctx context.Context, req *Request) (<-chan *Response, error) {
	responseCh := make(chan *Response, 1)

	// Format message based on stage
	text := h.formatApprovalMessage(req)

	// Create inline keyboard with approve/reject buttons
	keyboard := h.createApprovalKeyboard(req)

	// Resolve destination: use first approver's chat_id when available
	destChatID := h.chatID
	if len(req.Approvers) > 0 {
		destChatID = req.Approvers[0]
	}

	// Send message
	resp, err := h.client.SendMessageWithKeyboard(ctx, destChatID, text, "", keyboard)
	if err != nil {
		return nil, fmt.Errorf("failed to send Telegram message: %w", err)
	}

	// Track pending request
	var messageID int64
	if resp != nil && resp.Result != nil {
		messageID = resp.Result.MessageID
	}

	h.mu.Lock()
	h.pending[req.ID] = &telegramPending{
		Request:    req,
		MessageID:  messageID,
		ChatID:     destChatID,
		ResponseCh: responseCh,
	}
	h.mu.Unlock()

	// Best-effort persistence so the request survives a restart.
	if h.store != nil {
		row := &memory.PendingApproval{
			ID:               req.ID,
			TaskID:           req.TaskID,
			Stage:            string(req.Stage),
			Title:            req.Title,
			Description:      req.Description,
			Metadata:         req.Metadata,
			Approvers:        req.Approvers,
			PreferredChannel: req.PreferredChannel,
			CreatedAt:        req.CreatedAt,
			ExpiresAt:        req.ExpiresAt,
		}
		if err := h.store.InsertPendingApproval(row); err != nil {
			h.log.Warn("failed to persist pending approval", slog.String("request_id", req.ID), slog.Any("error", err))
		}
	}

	h.log.Debug("Sent approval request",
		slog.String("request_id", req.ID),
		slog.String("chat_id", destChatID),
		slog.Int64("message_id", messageID))

	return responseCh, nil
}

// CancelRequest cancels a pending approval request
func (h *TelegramHandler) CancelRequest(ctx context.Context, requestID string) error {
	h.mu.Lock()
	pending, exists := h.pending[requestID]
	if exists {
		delete(h.pending, requestID)
	}
	h.mu.Unlock()

	if !exists {
		return nil
	}

	if h.store != nil {
		if err := h.store.DeletePendingApproval(requestID); err != nil {
			h.log.Warn("failed to delete persisted approval on cancel", slog.String("request_id", requestID), slog.Any("error", err))
		}
	}

	// Update message to show cancelled
	if pending.MessageID != 0 {
		text := h.formatCancelledMessage(pending.Request)
		if err := h.client.EditMessage(ctx, pending.ChatID, pending.MessageID, text, ""); err != nil {
			h.log.Warn("Failed to edit cancelled message", slog.Any("error", err))
		}
	}

	// Close response channel
	close(pending.ResponseCh)

	return nil
}

// HandleCallback processes a Telegram callback (button press)
// This should be called by the main Telegram handler when receiving callbacks
func (h *TelegramHandler) HandleCallback(ctx context.Context, callbackID, data, userID, username string) bool {
	// Parse callback data: "approve:<requestID>" or "reject:<requestID>"
	var decision Decision
	var requestID string

	if len(data) > 8 && data[:8] == "approve:" {
		decision = DecisionApproved
		requestID = data[8:]
	} else if len(data) > 7 && data[:7] == "reject:" {
		decision = DecisionRejected
		requestID = data[7:]
	} else {
		return false // Not an approval callback
	}

	h.mu.Lock()
	pending, exists := h.pending[requestID]
	if exists {
		delete(h.pending, requestID)
	}
	h.mu.Unlock()

	if !exists {
		_ = h.client.AnswerCallback(ctx, callbackID, "Request expired or already processed")
		return true
	}

	if h.store != nil {
		if err := h.store.DeletePendingApproval(requestID); err != nil {
			h.log.Warn("failed to delete persisted approval on callback", slog.String("request_id", requestID), slog.Any("error", err))
		}
	}

	// Answer callback
	var answerText string
	if decision == DecisionApproved {
		answerText = "Approved!"
	} else {
		answerText = "Rejected"
	}
	_ = h.client.AnswerCallback(ctx, callbackID, answerText)

	// Update message to show result
	if pending.MessageID != 0 {
		text := h.formatResponseMessage(pending.Request, decision, username)
		if err := h.client.EditMessage(ctx, pending.ChatID, pending.MessageID, text, ""); err != nil {
			h.log.Warn("Failed to edit response message", slog.Any("error", err))
		}
	}

	// Send response
	response := &Response{
		RequestID:   requestID,
		Decision:    decision,
		ApprovedBy:  username,
		RespondedAt: time.Now(),
	}

	select {
	case pending.ResponseCh <- response:
	default:
	}
	close(pending.ResponseCh)

	h.log.Info("Approval callback handled",
		slog.String("request_id", requestID),
		slog.String("decision", string(decision)),
		slog.String("user", username))

	return true
}

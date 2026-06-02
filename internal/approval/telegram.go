package approval

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/memory"
)

// PendingApprovalStore persists pending approval requests across restarts.
// *memory.Store satisfies this interface directly.
type PendingApprovalStore interface {
	InsertPendingApproval(*memory.PendingApproval) error
	DeletePendingApproval(id string) error
	LoadPendingApprovals() ([]*memory.PendingApproval, error)
}

// TelegramClient defines the interface for Telegram operations
// This allows the approval handler to use the existing Telegram client
type TelegramClient interface {
	SendMessageWithKeyboard(ctx context.Context, chatID, text, parseMode string, keyboard [][]InlineKeyboardButton) (*MessageResponse, error)
	EditMessage(ctx context.Context, chatID string, messageID int64, text, parseMode string) error
	AnswerCallback(ctx context.Context, callbackID, text string) error
}

// InlineKeyboardButton represents a Telegram inline keyboard button
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// MessageResponse represents a Telegram API response with message result
type MessageResponse struct {
	Result *MessageResult `json:"result"`
}

// MessageResult contains the sent message details
type MessageResult struct {
	MessageID int64 `json:"message_id"`
}

// TelegramHandler handles approval requests via Telegram
type TelegramHandler struct {
	client  TelegramClient
	chatID  string
	pending map[string]*telegramPending // requestID -> pending state
	mu      sync.RWMutex
	log     *slog.Logger
	store   PendingApprovalStore // optional; enables restart persistence
}

// telegramPending tracks a pending Telegram approval request
type telegramPending struct {
	Request    *Request
	MessageID  int64
	ChatID     string // resolved destination — may differ from h.chatID when approvers are set
	ResponseCh chan *Response
}

// NewTelegramHandler creates a new Telegram approval handler
func NewTelegramHandler(client TelegramClient, chatID string) *TelegramHandler {
	return &TelegramHandler{
		client:  client,
		chatID:  chatID,
		pending: make(map[string]*telegramPending),
		log:     logging.WithComponent("approval.telegram"),
	}
}

// Name returns the handler name
func (h *TelegramHandler) Name() string {
	return "telegram"
}

// WithStore attaches a persistence store so pending approvals survive restarts.
// Returns h to allow builder-style chaining after NewTelegramHandler.
func (h *TelegramHandler) WithStore(store PendingApprovalStore) *TelegramHandler {
	h.store = store
	return h
}

// Rehydrate loads persisted pending approvals from the store and re-inserts them
// into the in-memory map so that button taps that arrive after a restart are
// processed rather than answered with "expired". Expired rows are pruned.
// No-op when no store is attached.
func (h *TelegramHandler) Rehydrate(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	rows, err := h.store.LoadPendingApprovals()
	if err != nil {
		return fmt.Errorf("rehydrate: load pending approvals: %w", err)
	}
	now := time.Now()
	rehydrated := 0
	for _, row := range rows {
		if row.ExpiresAt.Before(now) {
			_ = h.store.DeletePendingApproval(row.ID)
			continue
		}
		req := &Request{
			ID:               row.ID,
			TaskID:           row.TaskID,
			Stage:            Stage(row.Stage),
			Title:            row.Title,
			Description:      row.Description,
			Metadata:         row.Metadata,
			Approvers:        row.Approvers,
			PreferredChannel: row.PreferredChannel,
			CreatedAt:        row.CreatedAt,
			ExpiresAt:        row.ExpiresAt,
		}
		destChatID := h.chatID
		if len(req.Approvers) > 0 {
			destChatID = req.Approvers[0]
		}
		responseCh := make(chan *Response, 1)
		h.mu.Lock()
		if _, exists := h.pending[req.ID]; !exists {
			h.pending[req.ID] = &telegramPending{
				Request:    req,
				ChatID:     destChatID,
				ResponseCh: responseCh,
			}
			rehydrated++
		}
		h.mu.Unlock()
	}
	if rehydrated > 0 {
		h.log.Info("rehydrated pending approvals", slog.Int("count", rehydrated))
	}
	return nil
}

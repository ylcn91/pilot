package approval

import (
	"context"
	"sync"

	"github.com/ylcn91/pilot/internal/memory"
)

// mockTelegramClient implements TelegramClient for testing
type mockTelegramClient struct {
	mu             sync.Mutex
	sentMessages   []mockSentMessage
	editedMessages []mockEditedMessage
	answeredCbs    []mockAnsweredCallback
	sendError      error
	editError      error
	answerError    error
	nextMessageID  int64
}

type mockSentMessage struct {
	ChatID   string
	Text     string
	Keyboard [][]InlineKeyboardButton
}

type mockEditedMessage struct {
	ChatID    string
	MessageID int64
	Text      string
}

type mockAnsweredCallback struct {
	CallbackID string
	Text       string
}

func (m *mockTelegramClient) SendMessageWithKeyboard(ctx context.Context, chatID, text, parseMode string, keyboard [][]InlineKeyboardButton) (*MessageResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendError != nil {
		return nil, m.sendError
	}

	m.sentMessages = append(m.sentMessages, mockSentMessage{
		ChatID:   chatID,
		Text:     text,
		Keyboard: keyboard,
	})

	m.nextMessageID++
	return &MessageResponse{
		Result: &MessageResult{
			MessageID: m.nextMessageID,
		},
	}, nil
}

func (m *mockTelegramClient) EditMessage(ctx context.Context, chatID string, messageID int64, text, parseMode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.editError != nil {
		return m.editError
	}

	m.editedMessages = append(m.editedMessages, mockEditedMessage{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	})
	return nil
}

func (m *mockTelegramClient) AnswerCallback(ctx context.Context, callbackID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.answerError != nil {
		return m.answerError
	}

	m.answeredCbs = append(m.answeredCbs, mockAnsweredCallback{
		CallbackID: callbackID,
		Text:       text,
	})
	return nil
}

func (m *mockTelegramClient) getSentMessages() []mockSentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockSentMessage, len(m.sentMessages))
	copy(result, m.sentMessages)
	return result
}

func (m *mockTelegramClient) getEditedMessages() []mockEditedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockEditedMessage, len(m.editedMessages))
	copy(result, m.editedMessages)
	return result
}

func (m *mockTelegramClient) getAnsweredCallbacks() []mockAnsweredCallback {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockAnsweredCallback, len(m.answeredCbs))
	copy(result, m.answeredCbs)
	return result
}

// mockPendingStore is an in-memory PendingApprovalStore for unit tests.
type mockPendingStore struct {
	mu   sync.Mutex
	rows map[string]*memory.PendingApproval
	// per-call error injection
	insertErr error
	deleteErr error
	loadErr   error
}

func newMockPendingStore() *mockPendingStore {
	return &mockPendingStore{rows: make(map[string]*memory.PendingApproval)}
}

func (s *mockPendingStore) InsertPendingApproval(a *memory.PendingApproval) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *a
	s.rows[a.ID] = &cp
	return nil
}

func (s *mockPendingStore) DeletePendingApproval(id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}

func (s *mockPendingStore) LoadPendingApprovals() ([]*memory.PendingApproval, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*memory.PendingApproval, 0, len(s.rows))
	for _, r := range s.rows {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (s *mockPendingStore) get(id string) *memory.PendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id]
}

func (s *mockPendingStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// containsString is a helper to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

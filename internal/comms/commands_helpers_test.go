package comms

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

// mockMessenger captures messages sent by the command handler.
type mockMessenger struct {
	messages []string
}

func (m *mockMessenger) SendText(ctx context.Context, contextID, text string) error {
	m.messages = append(m.messages, text)
	return nil
}

func (m *mockMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, desc, project string) (string, error) {
	return "", nil
}

func (m *mockMessenger) SendProgress(ctx context.Context, contextID, messageRef, taskID, phase string, progress int, detail string) (string, error) {
	return "", nil
}

func (m *mockMessenger) SendResult(ctx context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	return nil
}

func (m *mockMessenger) SendChunked(ctx context.Context, contextID, threadID, content, prefix string) error {
	return nil
}

func (m *mockMessenger) AcknowledgeCallback(ctx context.Context, callbackID string) error {
	return nil
}

func (m *mockMessenger) MaxMessageLength() int {
	return 4096
}

// Helper functions

func containsString(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && stringContains(haystack, needle)
}

func stringContains(s, substr string) bool {
	return len(substr) <= len(s) && (substr == s || len(s) > 0 && len(substr) > 0)
}

func mustCreateMemoryStore(t *testing.T) *memory.Store {
	store, err := memory.NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	return store
}

package discord

import (
	"context"
	"sync"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// --- noopMessenger for tests ---

type noopMessenger struct {
	mu       sync.Mutex
	texts    []sentText
	confirms []sentConfirm
	results  []sentResult
	chunks   []sentChunk
	acks     []string
}

type sentText struct{ contextID, text string }
type sentConfirm struct{ contextID, threadID, taskID, desc, project string }
type sentResult struct {
	contextID, threadID, taskID string
	success                     bool
	output, prURL               string
}
type sentChunk struct{ contextID, threadID, content, prefix string }

func (n *noopMessenger) SendText(_ context.Context, contextID, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.texts = append(n.texts, sentText{contextID, text})
	return nil
}
func (n *noopMessenger) SendConfirmation(_ context.Context, contextID, threadID, taskID, desc, project string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.confirms = append(n.confirms, sentConfirm{contextID, threadID, taskID, desc, project})
	return "msg-ref-1", nil
}
func (n *noopMessenger) SendProgress(_ context.Context, _, messageRef, _, _ string, _ int, _ string) (string, error) {
	return messageRef, nil
}
func (n *noopMessenger) SendResult(_ context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.results = append(n.results, sentResult{contextID, threadID, taskID, success, output, prURL})
	return nil
}
func (n *noopMessenger) SendChunked(_ context.Context, contextID, threadID, content, prefix string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.chunks = append(n.chunks, sentChunk{contextID, threadID, content, prefix})
	return nil
}
func (n *noopMessenger) AcknowledgeCallback(_ context.Context, callbackID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.acks = append(n.acks, callbackID)
	return nil
}
func (n *noopMessenger) MaxMessageLength() int { return 2000 }

func newTestCommsHandler(m comms.Messenger) *comms.Handler {
	if m == nil {
		m = &noopMessenger{}
	}
	return comms.NewHandler(&comms.HandlerConfig{
		Messenger:    m,
		TaskIDPrefix: "DISCORD",
	})
}

func newTestHandler(ch *comms.Handler) *Handler {
	return NewHandler(&HandlerConfig{
		BotToken: testutil.FakeBearerToken,
	}, ch)
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func contains(haystack, needle string) bool {
	for i := 0; i < len(haystack)-len(needle)+1; i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

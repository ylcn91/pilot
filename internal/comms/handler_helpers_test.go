package comms

import (
	"context"
	"sync"

	"github.com/ylcn91/pilot/internal/intent"
)

// handlerMock records all Messenger calls for assertion in handler tests.
type handlerMock struct {
	mu       sync.Mutex
	texts    []hSentText
	confirms []hSentConfirm
	results  []hSentResult
	chunks   []hSentChunk
	progress []hSentProgress
	acks     []string
}

type hSentText struct {
	contextID, text string
}
type hSentConfirm struct {
	contextID, threadID, taskID, desc, project string
}
type hSentResult struct {
	contextID, threadID, taskID, output, prURL string
	success                                    bool
}
type hSentChunk struct {
	contextID, threadID, content, prefix string
}
type hSentProgress struct {
	contextID, msgRef, taskID, phase, detail string
	progress                                 int
}

func (m *handlerMock) SendText(_ context.Context, contextID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.texts = append(m.texts, hSentText{contextID, text})
	return nil
}

func (m *handlerMock) SendConfirmation(_ context.Context, contextID, threadID, taskID, desc, project string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirms = append(m.confirms, hSentConfirm{contextID, threadID, taskID, desc, project})
	return "msg-ref-1", nil
}

func (m *handlerMock) SendProgress(_ context.Context, contextID, msgRef, taskID, phase string, progress int, detail string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progress = append(m.progress, hSentProgress{contextID, msgRef, taskID, phase, detail, progress})
	return msgRef, nil
}

func (m *handlerMock) SendResult(_ context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = append(m.results, hSentResult{contextID, threadID, taskID, output, prURL, success})
	return nil
}

func (m *handlerMock) SendChunked(_ context.Context, contextID, threadID, content, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks = append(m.chunks, hSentChunk{contextID, threadID, content, prefix})
	return nil
}

func (m *handlerMock) AcknowledgeCallback(_ context.Context, callbackID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acks = append(m.acks, callbackID)
	return nil
}

func (m *handlerMock) MaxMessageLength() int { return 4000 }

func (m *handlerMock) getTexts() []hSentText {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]hSentText, len(m.texts))
	copy(cp, m.texts)
	return cp
}

// hMockClassifier returns a fixed intent.
type hMockClassifier struct {
	result intent.Intent
	err    error
}

func (c *hMockClassifier) Classify(_ context.Context, _ []intent.ConversationMessage, _ string) (intent.Intent, error) {
	return c.result, c.err
}

// hMockMemberResolver returns a fixed member ID.
type hMockMemberResolver struct {
	memberID string
	err      error
}

func (r *hMockMemberResolver) ResolveIdentity(_ string) (string, error) {
	return r.memberID, r.err
}

func newTestHandler(m *handlerMock) *Handler {
	return NewHandler(&HandlerConfig{
		Messenger:    m,
		TaskIDPrefix: "TEST",
	})
}

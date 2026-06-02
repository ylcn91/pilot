package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// noopMessenger is a no-op implementation of comms.Messenger for tests.
type noopMessenger struct{}

func (n *noopMessenger) SendText(context.Context, string, string) error { return nil }
func (n *noopMessenger) SendConfirmation(context.Context, string, string, string, string, string) (string, error) {
	return "", nil
}
func (n *noopMessenger) SendProgress(context.Context, string, string, string, string, int, string) (string, error) {
	return "", nil
}
func (n *noopMessenger) SendResult(context.Context, string, string, string, bool, string, string) error {
	return nil
}
func (n *noopMessenger) SendChunked(context.Context, string, string, string, string) error {
	return nil
}
func (n *noopMessenger) AcknowledgeCallback(context.Context, string) error { return nil }
func (n *noopMessenger) MaxMessageLength() int                             { return 4096 }

// newTestCommsHandler creates a comms.Handler with a no-op messenger for tests.
func newTestCommsHandler() *comms.Handler {
	return comms.NewHandler(&comms.HandlerConfig{
		Messenger:    &noopMessenger{},
		TaskIDPrefix: "TG",
	})
}

// MockRunner implements a minimal executor.Runner interface for testing
type MockRunner struct {
	cancelFunc     func(taskID string) error
	progressFunc   func(taskID, phase string, progress int, message string)
	progressMu     sync.Mutex
	onProgressFunc func(fn func(string, string, int, string))
}

func (m *MockRunner) OnProgress(fn func(string, string, int, string)) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	m.progressFunc = fn
	if m.onProgressFunc != nil {
		m.onProgressFunc(fn)
	}
}

func (m *MockRunner) Cancel(taskID string) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(taskID)
	}
	return nil
}

// MockProjectSource implements ProjectSource for testing
type MockProjectSource struct {
	projects []*comms.ProjectInfo
}

func (m *MockProjectSource) GetProjectByName(name string) *comms.ProjectInfo {
	for _, p := range m.projects {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func (m *MockProjectSource) GetProjectByPath(path string) *comms.ProjectInfo {
	for _, p := range m.projects {
		if p.Path == path {
			return p
		}
	}
	return nil
}

func (m *MockProjectSource) GetDefaultProject() *comms.ProjectInfo {
	if len(m.projects) > 0 {
		return m.projects[0]
	}
	return nil
}

func (m *MockProjectSource) ListProjects() []*comms.ProjectInfo {
	return m.projects
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockApprovalHandler records HandleCallback invocations for test assertions.
type mockApprovalHandler struct {
	calls []approvalCall
	mu    sync.Mutex
	ret   bool
}

type approvalCall struct {
	callbackID string
	data       string
	userID     string
	username   string
}

func (m *mockApprovalHandler) HandleCallback(_ context.Context, callbackID, data, userID, username string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, approvalCall{callbackID: callbackID, data: data, userID: userID, username: username})
	return m.ret
}

// newCallbackTestHandler creates a Handler backed by a test HTTP server that accepts answerCallbackQuery.
func newCallbackTestHandler(t *testing.T, approvalHandler ApprovalCallbackHandler) (*Handler, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	h := &Handler{
		client:          NewClientWithBaseURL(testutil.FakeTelegramBotToken, srv.URL),
		stopCh:          make(chan struct{}),
		approvalHandler: approvalHandler,
	}
	return h, srv.Close
}

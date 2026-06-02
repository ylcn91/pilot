package comms

import (
	"context"
	"testing"
	"time"
)

func TestNewHandler(t *testing.T) {
	m := &handlerMock{}
	h := NewHandler(&HandlerConfig{
		Messenger:    m,
		ProjectPath:  "/tmp/test-project",
		TaskIDPrefix: "TG",
	})

	if h.taskIDPrefix != "TG" {
		t.Errorf("expected prefix TG, got %s", h.taskIDPrefix)
	}
	if h.projectPath != "/tmp/test-project" {
		t.Errorf("expected project path /tmp/test-project, got %s", h.projectPath)
	}
	if h.rateLimit == nil {
		t.Error("expected rate limiter to be initialized")
	}
}

func TestNewHandler_DefaultPrefix(t *testing.T) {
	m := &handlerMock{}
	h := NewHandler(&HandlerConfig{Messenger: m})
	if h.taskIDPrefix != "MSG" {
		t.Errorf("expected default prefix MSG, got %s", h.taskIDPrefix)
	}
}

func TestHandleMessage_RateLimited(t *testing.T) {
	m := &handlerMock{}
	h := NewHandler(&HandlerConfig{
		Messenger: m,
		RateLimit: &RateLimitConfig{
			Enabled:           true,
			MessagesPerMinute: 1,
			BurstSize:         1,
			TasksPerHour:      1,
		},
		TaskIDPrefix: "TEST",
	})

	ctx := context.Background()
	// First message consumes the single token
	h.HandleMessage(ctx, &IncomingMessage{ContextID: "ch1", SenderID: "u1", Text: "hello"})
	// Second message should be rate limited
	h.HandleMessage(ctx, &IncomingMessage{ContextID: "ch1", SenderID: "u1", Text: "hello again"})

	texts := m.getTexts()
	found := false
	for _, st := range texts {
		if st.text == "⚠️ Rate limit exceeded. Please wait before sending more messages." {
			found = true
		}
	}
	if !found {
		t.Error("expected rate limit message")
	}
}

func TestHandleMessage_Greeting(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)

	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID: "ch1",
		SenderID:  "u1",
		Text:      "hello",
	})

	texts := m.getTexts()
	if len(texts) == 0 {
		t.Fatal("expected at least one text message")
	}
	if texts[0].text != "👋 Hello! I'm Pilot — send me a task, question, or say /help." {
		t.Errorf("unexpected greeting: %s", texts[0].text)
	}
}

func TestHandleMessage_ConfirmationNo(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)

	// Seed a pending task
	h.mu.Lock()
	h.pendingTasks["ch1"] = &PendingTask{
		TaskID:      "TEST-123",
		Description: "do something",
		ContextID:   "ch1",
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID: "ch1",
		SenderID:  "u1",
		Text:      "no",
	})

	texts := m.getTexts()
	if len(texts) == 0 {
		t.Fatal("expected cancellation message")
	}
	if texts[0].text != "❌ Task TEST-123 cancelled." {
		t.Errorf("unexpected message: %s", texts[0].text)
	}

	// Verify pending task was removed
	h.mu.Lock()
	_, exists := h.pendingTasks["ch1"]
	h.mu.Unlock()
	if exists {
		t.Error("pending task should have been removed")
	}
}

func TestHandleMessage_CallbackConfirmation(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)

	h.mu.Lock()
	h.pendingTasks["ch1"] = &PendingTask{
		TaskID:      "TEST-456",
		Description: "build feature",
		ContextID:   "ch1",
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID:  "ch1",
		SenderID:   "u1",
		IsCallback: true,
		CallbackID: "cb-1",
		ActionID:   "cancel",
	})

	if len(m.acks) == 0 {
		t.Error("expected callback acknowledgment")
	}
	texts := m.getTexts()
	if len(texts) == 0 {
		t.Fatal("expected cancellation message")
	}
	if texts[0].text != "❌ Task TEST-456 cancelled." {
		t.Errorf("unexpected: %s", texts[0].text)
	}
}

func TestHandleMessage_NoConfirmationPending(t *testing.T) {
	m := &handlerMock{}
	h := newTestHandler(m)

	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID: "ch1",
		SenderID:  "u1",
		Text:      "yes",
	})

	texts := m.getTexts()
	if len(texts) == 0 {
		t.Fatal("expected 'no pending task' message")
	}
	if texts[0].text != "No pending task to confirm." {
		t.Errorf("unexpected: %s", texts[0].text)
	}
}

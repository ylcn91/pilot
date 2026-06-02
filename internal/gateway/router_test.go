package gateway

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewRouter(t *testing.T) {
	router := NewRouter()

	if router == nil {
		t.Fatal("NewRouter returned nil")
	}
	if router.messageHandlers == nil {
		t.Error("messageHandlers not initialized")
	}
	if router.webhookHandlers == nil {
		t.Error("webhookHandlers not initialized")
	}

	// Verify ping handler is registered by default
	router.mu.RLock()
	_, hasPingHandler := router.messageHandlers[MessageTypePing]
	router.mu.RUnlock()

	if !hasPingHandler {
		t.Error("Ping handler should be registered by default")
	}
}

func TestRouterRegisterMessageHandler(t *testing.T) {
	router := NewRouter()

	handler := func(session *Session, payload json.RawMessage) {
		// Handler logic
	}

	router.RegisterMessageHandler(MessageTypeTask, handler)

	router.mu.RLock()
	handlers, ok := router.messageHandlers[MessageTypeTask]
	router.mu.RUnlock()

	if !ok {
		t.Error("Handler not registered")
	}
	if len(handlers) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(handlers))
	}
}

func TestRouterRegisterMultipleMessageHandlers(t *testing.T) {
	router := NewRouter()

	var callOrder []int
	var mu sync.Mutex

	handler1 := func(session *Session, payload json.RawMessage) {
		mu.Lock()
		callOrder = append(callOrder, 1)
		mu.Unlock()
	}
	handler2 := func(session *Session, payload json.RawMessage) {
		mu.Lock()
		callOrder = append(callOrder, 2)
		mu.Unlock()
	}

	router.RegisterMessageHandler(MessageTypeStatus, handler1)
	router.RegisterMessageHandler(MessageTypeStatus, handler2)

	router.mu.RLock()
	handlers := router.messageHandlers[MessageTypeStatus]
	router.mu.RUnlock()

	if len(handlers) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(handlers))
	}

	// Call handlers and verify order
	for _, h := range handlers {
		h(nil, nil)
	}

	if len(callOrder) != 2 {
		t.Errorf("Expected 2 calls, got %d", len(callOrder))
	}
	if callOrder[0] != 1 || callOrder[1] != 2 {
		t.Errorf("Handlers called in wrong order: %v", callOrder)
	}
}

func TestRouterRegisterWebhookHandler(t *testing.T) {
	router := NewRouter()

	handlerCalled := false
	handler := func(payload map[string]interface{}) {
		handlerCalled = true
	}

	router.RegisterWebhookHandler("linear", handler)

	router.mu.RLock()
	handlers, ok := router.webhookHandlers["linear"]
	router.mu.RUnlock()

	if !ok {
		t.Error("Webhook handler not registered")
	}
	if len(handlers) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(handlers))
	}

	// Call handler
	handlers[0](nil)
	if !handlerCalled {
		t.Error("Handler was not called")
	}
}

func TestRouterRegisterMultipleWebhookHandlers(t *testing.T) {
	router := NewRouter()

	var callCount int32

	handler1 := func(payload map[string]interface{}) {
		atomic.AddInt32(&callCount, 1)
	}
	handler2 := func(payload map[string]interface{}) {
		atomic.AddInt32(&callCount, 1)
	}

	router.RegisterWebhookHandler("github", handler1)
	router.RegisterWebhookHandler("github", handler2)

	router.mu.RLock()
	handlers := router.webhookHandlers["github"]
	router.mu.RUnlock()

	if len(handlers) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(handlers))
	}
}

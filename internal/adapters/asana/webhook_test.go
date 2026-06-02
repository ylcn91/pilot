package asana

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, testutil.FakeAsanaWebhookSecret, "pilot")

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if handler.pilotTag != "pilot" {
		t.Errorf("handler.pilotTag = %s, want pilot", handler.pilotTag)
	}
}

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"events":[]}`)
	secret := testutil.FakeAsanaWebhookSecret

	t.Run("valid HMAC signature", func(t *testing.T) {
		sig := computeAsanaHMAC(secret, payload)
		h := NewWebhookHandler(nil, secret, "pilot")
		if !h.VerifySignature(payload, sig) {
			t.Error("expected valid signature to pass")
		}
	})

	t.Run("wrong signature", func(t *testing.T) {
		h := NewWebhookHandler(nil, secret, "pilot")
		if h.VerifySignature(payload, "badhex") {
			t.Error("expected wrong signature to fail")
		}
	})

	t.Run("empty signature with valid secret", func(t *testing.T) {
		h := NewWebhookHandler(nil, secret, "pilot")
		if h.VerifySignature(payload, "") {
			t.Error("expected empty signature to fail when secret is configured")
		}
	})

	t.Run("empty secret fail-closed", func(t *testing.T) {
		h := NewWebhookHandler(nil, "", "pilot")
		if h.VerifySignature(payload, "any-signature") {
			t.Error("expected empty secret to fail closed without dev flag")
		}
	})

	t.Run("empty secret allowed with dev flag", func(t *testing.T) {
		t.Setenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS", "1")
		h := NewWebhookHandler(nil, "", "pilot")
		if !h.VerifySignature(payload, "any-signature") {
			t.Error("expected empty secret to pass when PILOT_ALLOW_UNSIGNED_WEBHOOKS=1")
		}
	})
}

// computeAsanaHMAC computes the expected HMAC-SHA256 signature for testing.
func computeAsanaHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHandleHandshake(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	hookSecret := "abc123secret"
	result := handler.HandleHandshake(hookSecret)

	if result != hookSecret {
		t.Errorf("HandleHandshake() = %s, want %s", result, hookSecret)
	}
}

func TestHandle_EmptyPayload(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	payload := &WebhookPayload{
		Events: []WebhookEvent{},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
}

func TestHandle_NonTaskEvent(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	// Set callback that should NOT be called
	callbackCalled := false
	handler.OnTask(func(ctx context.Context, task *Task) error {
		callbackCalled = true
		return nil
	})

	payload := &WebhookPayload{
		Events: []WebhookEvent{
			{
				Action: "added",
				Resource: WebhookResource{
					GID:          "123",
					ResourceType: "project", // Not a task
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if callbackCalled {
		t.Error("callback should not be called for non-task events")
	}
}

func TestHandle_TaskWithPilotTag(t *testing.T) {
	// Create mock server that returns task with pilot tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Name:      "Test Task",
				Completed: false,
				Tags: []Tag{
					{GID: "1", Name: "pilot"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	// Set callback
	var receivedTask *Task
	handler.OnTask(func(ctx context.Context, task *Task) error {
		receivedTask = task
		return nil
	})

	payload := &WebhookPayload{
		Events: []WebhookEvent{
			{
				Action: "added",
				Resource: WebhookResource{
					GID:          "123456",
					ResourceType: "task",
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if receivedTask == nil {
		t.Error("callback was not called")
	}
	if receivedTask != nil && receivedTask.GID != "123456" {
		t.Errorf("received task GID = %s, want 123456", receivedTask.GID)
	}
}

func TestHandle_TaskWithoutPilotTag(t *testing.T) {
	// Create mock server that returns task without pilot tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Name:      "Test Task",
				Completed: false,
				Tags: []Tag{
					{GID: "1", Name: "other-tag"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	// Set callback that should NOT be called
	callbackCalled := false
	handler.OnTask(func(ctx context.Context, task *Task) error {
		callbackCalled = true
		return nil
	})

	payload := &WebhookPayload{
		Events: []WebhookEvent{
			{
				Action: "added",
				Resource: WebhookResource{
					GID:          "123456",
					ResourceType: "task",
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if callbackCalled {
		t.Error("callback should not be called for tasks without pilot tag")
	}
}

func TestHandle_CompletedTask(t *testing.T) {
	// Create mock server that returns completed task
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse[Task]{
			Data: Task{
				GID:       "123456",
				Name:      "Test Task",
				Completed: true, // Already completed
				Tags: []Tag{
					{GID: "1", Name: "pilot"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	handler := NewWebhookHandler(client, "", "pilot")

	// Set callback that should NOT be called
	callbackCalled := false
	handler.OnTask(func(ctx context.Context, task *Task) error {
		callbackCalled = true
		return nil
	})

	payload := &WebhookPayload{
		Events: []WebhookEvent{
			{
				Action: "changed",
				Resource: WebhookResource{
					GID:          "123456",
					ResourceType: "task",
				},
			},
		},
	}

	err := handler.Handle(context.Background(), payload)
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
	if callbackCalled {
		t.Error("callback should not be called for completed tasks")
	}
}

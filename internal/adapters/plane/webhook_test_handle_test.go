package plane

import (
	"context"
	"errors"
	"testing"
)

func TestNewWebhookHandler(t *testing.T) {
	h := NewWebhookHandler("secret", "pilot-label-uuid", []string{"proj-1"})
	if h == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if h.secret != "secret" {
		t.Errorf("secret = %s, want 'secret'", h.secret)
	}
	if h.pilotLabel != "pilot-label-uuid" {
		t.Errorf("pilotLabel = %s, want 'pilot-label-uuid'", h.pilotLabel)
	}
	if len(h.projectIDs) != 1 {
		t.Errorf("projectIDs length = %d, want 1", len(h.projectIDs))
	}
	if h.onWorkItem != nil {
		t.Error("onWorkItem should be nil initially")
	}
}

func TestHandle_IssueCreated_WithPilotLabel(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var received *WebhookWorkItemData
	h.OnWorkItem(func(_ context.Context, data *WebhookWorkItemData) error {
		received = data
		return nil
	})

	data := WebhookWorkItemData{
		ID:        "wi-1",
		Name:      "Fix login bug",
		StateID:   "state-1",
		LabelIDs:  []string{"other-label", "pilot-label-uuid"},
		ProjectID: "proj-1",
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if received == nil {
		t.Fatal("callback was not called")
	}
	if received.ID != "wi-1" {
		t.Errorf("work item ID = %s, want 'wi-1'", received.ID)
	}
	if received.Name != "Fix login bug" {
		t.Errorf("work item Name = %s, want 'Fix login bug'", received.Name)
	}
}

func TestHandle_IssueUpdated_WithPilotLabel(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:       "wi-1",
		Name:     "Fix login bug",
		LabelIDs: []string{"pilot-label-uuid"},
	}
	payload := makePayload(t, "issue", "updated", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if !called {
		t.Error("callback should be called for updated action")
	}
}

func TestHandle_InvalidSignature(t *testing.T) {
	h := NewWebhookHandler("real-secret", "pilot-label-uuid", nil)

	payload := makePayload(t, "issue", "created", WebhookWorkItemData{
		ID:       "wi-1",
		LabelIDs: []string{"pilot-label-uuid"},
	})

	err := h.Handle(context.Background(), payload, "bad-sig")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if err.Error() != "invalid webhook signature" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandle_NoPilotLabel(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:       "wi-1",
		LabelIDs: []string{"bug-label", "feature-label"},
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if called {
		t.Error("callback should not be called without pilot label")
	}
}

func TestHandle_WrongProject(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", []string{"allowed-project"})

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:        "wi-1",
		LabelIDs:  []string{"pilot-label-uuid"},
		ProjectID: "other-project",
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if called {
		t.Error("callback should not be called for non-allowed project")
	}
}

func TestHandle_AllowedProject(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", []string{"proj-a", "proj-b"})

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:        "wi-1",
		LabelIDs:  []string{"pilot-label-uuid"},
		ProjectID: "proj-b",
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if !called {
		t.Error("callback should be called for allowed project")
	}
}

func TestHandle_NoProjectFilter(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:        "wi-1",
		LabelIDs:  []string{"pilot-label-uuid"},
		ProjectID: "any-project",
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if !called {
		t.Error("callback should be called when no project filter is set")
	}
}

func TestHandle_NonIssueEvent(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{ID: "wi-1", LabelIDs: []string{"pilot-label-uuid"}}
	payload := makePayload(t, "project", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if called {
		t.Error("callback should not be called for non-issue events")
	}
}

func TestHandle_DeletedAction(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{ID: "wi-1", LabelIDs: []string{"pilot-label-uuid"}}
	payload := makePayload(t, "issue", "deleted", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if called {
		t.Error("callback should not be called for deleted action")
	}
}

func TestHandle_CallbackError(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)

	expectedErr := errors.New("processing failed")
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		return expectedErr
	})

	data := WebhookWorkItemData{
		ID:       "wi-1",
		LabelIDs: []string{"pilot-label-uuid"},
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != expectedErr {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}

func TestHandle_NoCallback(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "pilot-label-uuid", nil)
	// No callback set

	data := WebhookWorkItemData{
		ID:       "wi-1",
		LabelIDs: []string{"pilot-label-uuid"},
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestHandle_InvalidJSON(t *testing.T) {
	h := NewWebhookHandler("", "pilot-label-uuid", nil)

	err := h.Handle(context.Background(), []byte(`{invalid`), "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHandle_EmptyPilotLabel(t *testing.T) {
	secret := "test-secret"
	h := NewWebhookHandler(secret, "", nil)

	var called bool
	h.OnWorkItem(func(_ context.Context, _ *WebhookWorkItemData) error {
		called = true
		return nil
	})

	data := WebhookWorkItemData{
		ID:       "wi-1",
		LabelIDs: []string{"some-label"},
	}
	payload := makePayload(t, "issue", "created", data)
	sig := computeSignature(secret, payload)

	err := h.Handle(context.Background(), payload, sig)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if called {
		t.Error("callback should not be called when pilot label is empty")
	}
}

package webhooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManager_AddRemoveEndpoint(t *testing.T) {
	manager := NewManager(nil, nil)

	// Add endpoint
	ep := &EndpointConfig{
		Name:    "Test",
		URL:     "https://example.com/hook",
		Enabled: true,
	}
	manager.AddEndpoint(ep)

	// Verify ID was generated
	if ep.ID == "" {
		t.Error("expected ID to be generated")
	}

	// Get endpoint
	retrieved := manager.GetEndpoint(ep.ID)
	if retrieved == nil {
		t.Fatal("expected to find endpoint")
	}
	if retrieved.Name != "Test" {
		t.Errorf("expected name 'Test', got '%s'", retrieved.Name)
	}

	// List endpoints
	list := manager.ListEndpoints()
	if len(list) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(list))
	}

	// Remove endpoint
	if !manager.RemoveEndpoint(ep.ID) {
		t.Error("expected removal to succeed")
	}

	// Verify removed
	if manager.GetEndpoint(ep.ID) != nil {
		t.Error("endpoint should be removed")
	}

	// Remove non-existent
	if manager.RemoveEndpoint("non-existent") {
		t.Error("expected removal of non-existent to return false")
	}
}

func TestManager_Stats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{
		Enabled: true,
		Endpoints: []*EndpointConfig{
			{
				ID:      "ep_test",
				URL:     server.URL,
				Enabled: true,
			},
		},
	}

	manager := NewManager(config, nil)
	ctx := context.Background()

	// Initial stats
	deliveries, failures, retries, lastDelivery := manager.Stats()
	if deliveries != 0 || failures != 0 || retries != 0 || !lastDelivery.IsZero() {
		t.Error("expected zero initial stats")
	}

	// Dispatch successful event
	manager.Dispatch(ctx, NewEvent(EventTaskCompleted, nil))

	deliveries, _, _, lastDelivery = manager.Stats()
	if deliveries != 1 {
		t.Errorf("expected 1 delivery, got %d", deliveries)
	}
	if lastDelivery.IsZero() {
		t.Error("expected lastDelivery to be set")
	}
}

func TestEndpointConfig_SubscribesTo(t *testing.T) {
	tests := []struct {
		name      string
		events    []EventType
		checkType EventType
		expected  bool
	}{
		{
			name:      "empty events means all",
			events:    []EventType{},
			checkType: EventTaskCompleted,
			expected:  true,
		},
		{
			name:      "subscribed event",
			events:    []EventType{EventTaskCompleted, EventTaskFailed},
			checkType: EventTaskCompleted,
			expected:  true,
		},
		{
			name:      "unsubscribed event",
			events:    []EventType{EventTaskCompleted},
			checkType: EventTaskFailed,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := &EndpointConfig{Events: tt.events}
			if got := ep.SubscribesTo(tt.checkType); got != tt.expected {
				t.Errorf("SubscribesTo() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEndpointConfig_GetTimeout(t *testing.T) {
	// With endpoint timeout
	ep := &EndpointConfig{Timeout: 5 * time.Second}
	if got := ep.GetTimeout(nil); got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}

	// With defaults
	ep = &EndpointConfig{}
	defaults := &EndpointDefaults{Timeout: 10 * time.Second}
	if got := ep.GetTimeout(defaults); got != 10*time.Second {
		t.Errorf("expected 10s, got %v", got)
	}

	// Fallback to default
	ep = &EndpointConfig{}
	if got := ep.GetTimeout(nil); got != 30*time.Second {
		t.Errorf("expected 30s default, got %v", got)
	}
}

func TestNewEvent(t *testing.T) {
	data := &TaskCompletedData{
		TaskID: "task-123",
		Title:  "Test",
	}

	event := NewEvent(EventTaskCompleted, data)

	if event.ID == "" {
		t.Error("expected event ID to be generated")
	}
	if !startsWith(event.ID, "evt_") {
		t.Errorf("expected event ID to start with 'evt_', got '%s'", event.ID)
	}
	if event.Type != EventTaskCompleted {
		t.Errorf("expected type %s, got %s", EventTaskCompleted, event.Type)
	}
	if event.Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
	if event.Data == nil {
		t.Error("expected data to be set")
	}
}

func TestAllEventTypes_IncludesTimeout(t *testing.T) {
	allTypes := AllEventTypes()

	// Check that task.timeout is included
	found := false
	for _, et := range allTypes {
		if et == EventTaskTimeout {
			found = true
			break
		}
	}

	if !found {
		t.Error("AllEventTypes() should include EventTaskTimeout")
	}
}

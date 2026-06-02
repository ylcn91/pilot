package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// PagerDutyChannel Tests (without real API)
// =============================================================================

func TestPagerDutyChannel_Name(t *testing.T) {
	config := &PagerDutyChannelConfig{
		RoutingKey: "routing-key",
		ServiceID:  "service-id",
	}

	ch := NewPagerDutyChannel("my-pagerduty", config)

	if ch.Name() != "my-pagerduty" {
		t.Errorf("expected name 'my-pagerduty', got '%s'", ch.Name())
	}
	if ch.Type() != "pagerduty" {
		t.Errorf("expected type 'pagerduty', got '%s'", ch.Type())
	}
}

func TestPagerDutyChannel_Send(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	ch := &PagerDutyChannel{
		name:       "test-pd",
		routingKey: testutil.FakePagerDutyRoutingKey,
		serviceID:  "test-service-id",
		baseURL:    server.URL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	alert := &Alert{
		Title:       "Test Alert",
		Message:     "Something broke",
		Severity:    SeverityCritical,
		Type:        AlertTypeTaskFailed,
		Source:      "test-project",
		ProjectPath: "/path/to/project",
		CreatedAt:   time.Now(),
		Metadata:    map[string]string{"task_id": "TASK-42"},
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	// Verify request body
	if receivedBody["routing_key"] != testutil.FakePagerDutyRoutingKey {
		t.Errorf("routing_key = %v, want %q", receivedBody["routing_key"], testutil.FakePagerDutyRoutingKey)
	}
	if receivedBody["event_action"] != "trigger" {
		t.Errorf("event_action = %v, want 'trigger'", receivedBody["event_action"])
	}

	payload, ok := receivedBody["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("payload missing or wrong type")
	}
	if payload["severity"] != "critical" {
		t.Errorf("severity = %v, want 'critical'", payload["severity"])
	}
	if payload["component"] != "pilot" {
		t.Errorf("component = %v, want 'pilot'", payload["component"])
	}
}

func TestPagerDutyChannel_Send_SeverityMapping(t *testing.T) {
	tests := []struct {
		name             string
		severity         Severity
		expectedSeverity string
	}{
		{"critical maps to critical", SeverityCritical, "critical"},
		{"warning maps to warning", SeverityWarning, "warning"},
		{"info maps to info", SeverityInfo, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &receivedBody)
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			ch := &PagerDutyChannel{
				name:       "pd-sev-test",
				routingKey: testutil.FakePagerDutyRoutingKey,
				baseURL:    server.URL,
				client:     &http.Client{},
			}

			alert := &Alert{
				Title:     "Severity Test",
				Message:   "Testing severity mapping",
				Severity:  tt.severity,
				Type:      AlertTypeTaskFailed,
				Source:    "test",
				CreatedAt: time.Now(),
			}

			err := ch.Send(context.Background(), alert)
			if err != nil {
				t.Fatalf("Send() error: %v", err)
			}

			payload, ok := receivedBody["payload"].(map[string]interface{})
			if !ok {
				t.Fatal("payload missing or wrong type")
			}
			if payload["severity"] != tt.expectedSeverity {
				t.Errorf("severity = %v, want %s", payload["severity"], tt.expectedSeverity)
			}
		})
	}
}

func TestPagerDutyChannel_Send_DedupKey(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	ch := &PagerDutyChannel{
		name:       "pd-dedup",
		routingKey: testutil.FakePagerDutyRoutingKey,
		baseURL:    server.URL,
		client:     &http.Client{},
	}

	alert := &Alert{
		Type:      AlertTypeConsecutiveFails,
		Source:    "my-project",
		Severity:  SeverityCritical,
		Title:     "Dedup Test",
		Message:   "Test",
		CreatedAt: time.Now(),
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	expectedDedup := "pilot-consecutive_failures-my-project"
	if receivedBody["dedup_key"] != expectedDedup {
		t.Errorf("dedup_key = %v, want %s", receivedBody["dedup_key"], expectedDedup)
	}
}

func TestPagerDutyChannel_Send_PayloadStructure(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	ch := &PagerDutyChannel{
		name:       "pd-structure",
		routingKey: testutil.FakePagerDutyRoutingKey,
		serviceID:  "test-service",
		baseURL:    server.URL,
		client:     &http.Client{},
	}

	alert := &Alert{
		Type:        AlertTypeTaskStuck,
		Source:      "task:TASK-42",
		Severity:    SeverityWarning,
		Title:       "Task Stuck",
		Message:     "No progress for 10m",
		ProjectPath: "/home/user/project",
		CreatedAt:   time.Now(),
		Metadata:    map[string]string{"task_id": "TASK-42", "phase": "build"},
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	// Verify top-level fields
	if receivedBody["routing_key"] != testutil.FakePagerDutyRoutingKey {
		t.Errorf("routing_key = %v", receivedBody["routing_key"])
	}
	if receivedBody["event_action"] != "trigger" {
		t.Errorf("event_action = %v", receivedBody["event_action"])
	}

	// Verify payload structure
	payload, ok := receivedBody["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("payload missing")
	}

	if payload["component"] != "pilot" {
		t.Errorf("component = %v, want 'pilot'", payload["component"])
	}
	if payload["group"] != "/home/user/project" {
		t.Errorf("group = %v, want '/home/user/project'", payload["group"])
	}
	if payload["class"] != string(AlertTypeTaskStuck) {
		t.Errorf("class = %v, want '%s'", payload["class"], AlertTypeTaskStuck)
	}
	if payload["source"] != "task:TASK-42" {
		t.Errorf("source = %v, want 'task:TASK-42'", payload["source"])
	}

	// Verify custom_details contains metadata
	details, ok := payload["custom_details"].(map[string]interface{})
	if !ok {
		t.Fatal("custom_details missing or wrong type")
	}
	if details["task_id"] != "TASK-42" {
		t.Errorf("custom_details.task_id = %v, want 'TASK-42'", details["task_id"])
	}
	if details["phase"] != "build" {
		t.Errorf("custom_details.phase = %v, want 'build'", details["phase"])
	}
}

func TestPagerDutyChannel_Send_NetworkError(t *testing.T) {
	ch := &PagerDutyChannel{
		name:       "pd-net-err",
		routingKey: testutil.FakePagerDutyRoutingKey,
		baseURL:    "http://localhost:99999",
		client:     &http.Client{},
	}

	err := ch.Send(context.Background(), &Alert{
		Title:     "Test",
		Message:   "fail",
		Severity:  SeverityWarning,
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestPagerDutyChannel_Send_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	ch := &PagerDutyChannel{
		name:       "test-pd",
		routingKey: testutil.FakePagerDutyRoutingKey,
		baseURL:    server.URL,
		client:     &http.Client{Timeout: 5 * time.Second},
	}

	err := ch.Send(context.Background(), &Alert{
		Title:     "Test",
		Message:   "fail",
		Severity:  SeverityWarning,
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

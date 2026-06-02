package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// =============================================================================
// WebhookChannel Tests
// =============================================================================

func TestNewWebhookChannel(t *testing.T) {
	tests := []struct {
		name         string
		config       *WebhookChannelConfig
		expectMethod string
	}{
		{
			name: "default method POST",
			config: &WebhookChannelConfig{
				URL:    "https://example.com/webhook",
				Method: "",
			},
			expectMethod: http.MethodPost,
		},
		{
			name: "explicit POST",
			config: &WebhookChannelConfig{
				URL:    "https://example.com/webhook",
				Method: "POST",
			},
			expectMethod: http.MethodPost,
		},
		{
			name: "explicit PUT",
			config: &WebhookChannelConfig{
				URL:    "https://example.com/webhook",
				Method: "PUT",
			},
			expectMethod: http.MethodPut,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewWebhookChannel("test", tt.config)

			if ch.Name() != "test" {
				t.Errorf("expected name 'test', got '%s'", ch.Name())
			}
			if ch.Type() != "webhook" {
				t.Errorf("expected type 'webhook', got '%s'", ch.Type())
			}
			if ch.method != tt.expectMethod {
				t.Errorf("expected method '%s', got '%s'", tt.expectMethod, ch.method)
			}
		})
	}
}

func TestWebhookChannel_Send(t *testing.T) {
	var receivedRequest *http.Request
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequest = r
		receivedBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookChannelConfig{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}

	ch := NewWebhookChannel("test-webhook", config)

	alert := &Alert{
		ID:       "alert-123",
		Type:     AlertTypeTaskFailed,
		Severity: SeverityWarning,
		Title:    "Test Alert",
		Message:  "Test message",
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify request
	if receivedRequest.Method != "POST" {
		t.Errorf("expected POST method, got %s", receivedRequest.Method)
	}
	if receivedRequest.Header.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type: application/json")
	}
	if receivedRequest.Header.Get("X-Custom-Header") != "custom-value" {
		t.Error("expected custom header")
	}
}

func TestWebhookChannel_Send_WithSignature(t *testing.T) {
	var receivedSignature string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookChannelConfig{
		URL:    server.URL,
		Secret: "my-secret-key",
	}

	ch := NewWebhookChannel("test-webhook", config)

	alert := &Alert{
		ID:   "alert-123",
		Type: AlertTypeTaskFailed,
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if receivedSignature == "" {
		t.Error("expected signature header to be set")
	}
	if len(receivedSignature) < 10 {
		t.Error("expected signature to have reasonable length")
	}
}

func TestWebhookChannel_Send_ErrorStatus(t *testing.T) {
	statusCodes := []int{400, 401, 403, 404, 500, 503}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			config := &WebhookChannelConfig{URL: server.URL}
			ch := NewWebhookChannel("test", config)

			alert := &Alert{ID: "test"}
			err := ch.Send(context.Background(), alert)

			if err == nil {
				t.Errorf("expected error for status %d", code)
			}
		})
	}
}

func TestWebhookChannel_Send_NetworkError(t *testing.T) {
	config := &WebhookChannelConfig{
		URL: "http://localhost:99999", // Invalid port
	}

	ch := NewWebhookChannel("test", config)

	alert := &Alert{ID: "test"}
	err := ch.Send(context.Background(), alert)

	if err == nil {
		t.Error("expected network error")
	}
}

func TestWebhookChannel_Sign(t *testing.T) {
	config := &WebhookChannelConfig{
		URL:    "https://example.com",
		Secret: testutil.FakeWebhookSecret,
	}

	ch := NewWebhookChannel("test", config)

	payload := []byte(`{"test": "data"}`)
	signature := ch.sign(payload)

	if signature == "" {
		t.Error("expected non-empty signature")
	}

	// Same payload should produce same signature
	signature2 := ch.sign(payload)
	if signature != signature2 {
		t.Error("expected same signature for same payload")
	}

	// Different payload should produce different signature
	signature3 := ch.sign([]byte(`{"different": "data"}`))
	if signature == signature3 {
		t.Error("expected different signature for different payload")
	}
}

func TestWebhookChannel_Send_VerifyHMACSignature(t *testing.T) {
	var receivedSignature string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Signature-256")
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := testutil.FakeWebhookSecret
	config := &WebhookChannelConfig{
		URL:    server.URL,
		Secret: secret,
	}

	ch := NewWebhookChannel("hmac-test", config)

	alert := &Alert{
		ID:       "alert-hmac",
		Type:     AlertTypeTaskFailed,
		Severity: SeverityCritical,
		Title:    "HMAC Verification Test",
		Message:  "Verify the signature is correct",
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	// Verify signature format: "sha256=<hex>"
	if receivedSignature == "" {
		t.Fatal("expected X-Signature-256 header")
	}
	if len(receivedSignature) < 7 || receivedSignature[:7] != "sha256=" {
		t.Fatalf("expected signature prefix 'sha256=', got '%s'", receivedSignature)
	}

	// Recompute the expected HMAC
	expectedSig := ch.sign(receivedBody)
	actualSig := receivedSignature[7:] // strip "sha256=" prefix

	if actualSig != expectedSig {
		t.Errorf("HMAC mismatch: got %s, expected %s", actualSig, expectedSig)
	}
}

func TestWebhookChannel_Send_NoSignatureWithoutSecret(t *testing.T) {
	var receivedSignature string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookChannelConfig{
		URL:    server.URL,
		Secret: "", // No secret
	}
	ch := NewWebhookChannel("no-sig", config)

	err := ch.Send(context.Background(), &Alert{ID: "no-sig-test"})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	if receivedSignature != "" {
		t.Errorf("expected no signature header, got '%s'", receivedSignature)
	}
}

func TestWebhookChannel_Send_CustomMethod(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookChannelConfig{
		URL:    server.URL,
		Method: "PUT",
	}
	ch := NewWebhookChannel("put-webhook", config)

	err := ch.Send(context.Background(), &Alert{ID: "put-test"})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	if receivedMethod != "PUT" {
		t.Errorf("expected PUT, got %s", receivedMethod)
	}
}

func TestWebhookChannel_Send_PayloadContainsAlert(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &WebhookChannelConfig{URL: server.URL}
	ch := NewWebhookChannel("payload-test", config)

	alert := &Alert{
		ID:          "alert-payload",
		Type:        AlertTypeConsecutiveFails,
		Severity:    SeverityCritical,
		Title:       "Consecutive Failures",
		Message:     "3 tasks failed in a row",
		Source:      "project-x",
		ProjectPath: "/path/to/project",
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	if receivedBody["id"] != "alert-payload" {
		t.Errorf("expected id 'alert-payload', got %v", receivedBody["id"])
	}
	if receivedBody["type"] != string(AlertTypeConsecutiveFails) {
		t.Errorf("expected type '%s', got %v", AlertTypeConsecutiveFails, receivedBody["type"])
	}
	if receivedBody["severity"] != string(SeverityCritical) {
		t.Errorf("expected severity 'critical', got %v", receivedBody["severity"])
	}
	if receivedBody["title"] != "Consecutive Failures" {
		t.Errorf("expected title 'Consecutive Failures', got %v", receivedBody["title"])
	}
	if receivedBody["source"] != "project-x" {
		t.Errorf("expected source 'project-x', got %v", receivedBody["source"])
	}
}

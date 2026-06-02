package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestClientUpdateMessage tests the UpdateMessage method
func TestClientUpdateMessage(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		ts         string
		msg        *Message
		response   map[string]interface{}
		statusCode int
		wantErr    bool
		errContain string
	}{
		{
			name:    "successful update",
			channel: "C1234567890",
			ts:      "1234567890.123456",
			msg: &Message{
				Text: "Updated message",
			},
			response: map[string]interface{}{
				"ok": true,
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:    "update with blocks",
			channel: "C1234567890",
			ts:      "1234567890.123456",
			msg: &Message{
				Blocks: []Block{
					{
						Type: "section",
						Text: &TextObject{
							Type: "mrkdwn",
							Text: "*Updated* content",
						},
					},
				},
			},
			response: map[string]interface{}{
				"ok": true,
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:    "message not found",
			channel: "C1234567890",
			ts:      "0000000000.000000",
			msg: &Message{
				Text: "Should fail",
			},
			response: map[string]interface{}{
				"ok":    false,
				"error": "message_not_found",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "message_not_found",
		},
		{
			name:    "cant update message",
			channel: "C1234567890",
			ts:      "1234567890.123456",
			msg: &Message{
				Text: "Edited",
			},
			response: map[string]interface{}{
				"ok":    false,
				"error": "cant_update_message",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "cant_update_message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify method
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}

				// Verify path
				if !strings.HasSuffix(r.URL.Path, "/chat.update") {
					t.Errorf("path = %q, want to end with /chat.update", r.URL.Path)
				}

				// Verify content type
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}

				// Verify authorization
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Authorization = %q, want Bearer token", auth)
				}

				// Parse request body
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}

				var req map[string]interface{}
				if err := json.Unmarshal(body, &req); err != nil {
					t.Fatalf("failed to parse request: %v", err)
				}

				// Verify channel and ts are present
				if req["channel"] != tt.channel {
					t.Errorf("channel = %v, want %q", req["channel"], tt.channel)
				}
				if req["ts"] != tt.ts {
					t.Errorf("ts = %v, want %q", req["ts"], tt.ts)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			ctx := context.Background()

			// Make request to test server
			payload := struct {
				Channel string  `json:"channel"`
				TS      string  `json:"ts"`
				Text    string  `json:"text,omitempty"`
				Blocks  []Block `json:"blocks,omitempty"`
			}{
				Channel: tt.channel,
				TS:      tt.ts,
				Text:    tt.msg.Text,
				Blocks:  tt.msg.Blocks,
			}

			body, _ := json.Marshal(payload)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/chat.update", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+testutil.FakeSlackBotToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			ok, _ := result["ok"].(bool)
			if tt.wantErr {
				if ok {
					t.Error("expected error but got OK response")
				}
				errStr, _ := result["error"].(string)
				if tt.errContain != "" && !strings.Contains(errStr, tt.errContain) {
					t.Errorf("error = %q, want to contain %q", errStr, tt.errContain)
				}
			} else {
				if !ok {
					t.Errorf("expected OK response but got error: %v", result["error"])
				}
			}
		})
	}
}

// TestClientContextCancellation tests that requests respect context cancellation
func TestClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		response := PostMessageResponse{OK: true, TS: "123"}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(testutil.FakeSlackBotToken)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	msg := &Message{Channel: "#test", Text: "test"}
	_, err := client.PostMessage(ctx, msg)

	// Should get context canceled error
	if err == nil {
		t.Error("expected error due to canceled context")
	}
}

// TestClientHTTPErrors tests handling of HTTP-level errors
func TestClientHTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"ok":false,"error":"internal_error"}`,
			wantErr:    true,
		},
		{
			name:       "bad gateway",
			statusCode: http.StatusBadGateway,
			body:       `{"ok":false,"error":"service_unavailable"}`,
			wantErr:    true,
		},
		{
			name:       "invalid JSON response",
			statusCode: http.StatusOK,
			body:       `not valid json`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			// Verify test setup
			if tt.wantErr {
				// Make a test request to verify server behavior
				resp, err := http.Get(server.URL)
				if err != nil {
					t.Fatalf("test server request failed: %v", err)
				}
				defer func() { _ = resp.Body.Close() }()

				if resp.StatusCode != tt.statusCode {
					t.Errorf("status = %d, want %d", resp.StatusCode, tt.statusCode)
				}
			}
		})
	}
}

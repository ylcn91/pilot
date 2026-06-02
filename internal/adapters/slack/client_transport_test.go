package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestClientPostMessageWithMockTransport tests PostMessage with injected transport
func TestClientPostMessageWithMockTransport(t *testing.T) {
	tests := []struct {
		name       string
		msg        *Message
		response   PostMessageResponse
		httpStatus int
		wantErr    bool
		errContain string
	}{
		{
			name: "successful post",
			msg: &Message{
				Channel: "#general",
				Text:    "Hello, world!",
			},
			response: PostMessageResponse{
				OK:      true,
				TS:      "1234567890.123456",
				Channel: "C1234567890",
			},
			httpStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name: "API error - channel not found",
			msg: &Message{
				Channel: "#nonexistent",
				Text:    "Test",
			},
			response: PostMessageResponse{
				OK:    false,
				Error: "channel_not_found",
			},
			httpStatus: http.StatusOK,
			wantErr:    true,
			errContain: "channel_not_found",
		},
		{
			name: "API error - rate limited",
			msg: &Message{
				Channel: "#general",
				Text:    "Test",
			},
			response: PostMessageResponse{
				OK:    false,
				Error: "rate_limited",
			},
			httpStatus: http.StatusOK,
			wantErr:    true,
			errContain: "rate_limited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock transport
			transport := &mockTransport{
				handler: func(req *http.Request) (*http.Response, error) {
					// Verify request
					if req.Method != http.MethodPost {
						t.Errorf("method = %q, want POST", req.Method)
					}
					if !strings.HasSuffix(req.URL.Path, "/chat.postMessage") {
						t.Errorf("path = %q, want /chat.postMessage suffix", req.URL.Path)
					}
					if ct := req.Header.Get("Content-Type"); ct != "application/json" {
						t.Errorf("Content-Type = %q, want application/json", ct)
					}
					if auth := req.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
						t.Errorf("Authorization = %q, want Bearer prefix", auth)
					}

					// Create response
					respBody, _ := json.Marshal(tt.response)
					return &http.Response{
						StatusCode: tt.httpStatus,
						Body:       io.NopCloser(strings.NewReader(string(respBody))),
						Header:     make(http.Header),
					}, nil
				},
			}

			client := &Client{
				botToken: testutil.FakeSlackBotToken,
				httpClient: &http.Client{
					Transport: transport,
					Timeout:   30 * time.Second,
				},
			}

			ctx := context.Background()
			result, err := client.PostMessage(ctx, tt.msg)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Error("result is nil")
				} else if result.TS != tt.response.TS {
					t.Errorf("TS = %q, want %q", result.TS, tt.response.TS)
				}
			}
		})
	}
}

// TestClientUpdateMessageWithMockTransport tests UpdateMessage with injected transport
func TestClientUpdateMessageWithMockTransport(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		ts         string
		msg        *Message
		response   map[string]interface{}
		httpStatus int
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
			httpStatus: http.StatusOK,
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
			httpStatus: http.StatusOK,
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
			httpStatus: http.StatusOK,
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
			httpStatus: http.StatusOK,
			wantErr:    true,
			errContain: "cant_update_message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock transport
			transport := &mockTransport{
				handler: func(req *http.Request) (*http.Response, error) {
					// Verify request
					if req.Method != http.MethodPost {
						t.Errorf("method = %q, want POST", req.Method)
					}
					if !strings.HasSuffix(req.URL.Path, "/chat.update") {
						t.Errorf("path = %q, want /chat.update suffix", req.URL.Path)
					}
					if ct := req.Header.Get("Content-Type"); ct != "application/json" {
						t.Errorf("Content-Type = %q, want application/json", ct)
					}
					if auth := req.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
						t.Errorf("Authorization = %q, want Bearer prefix", auth)
					}

					// Verify body contains channel and ts
					body, _ := io.ReadAll(req.Body)
					var reqBody map[string]interface{}
					_ = json.Unmarshal(body, &reqBody)
					if reqBody["channel"] != tt.channel {
						t.Errorf("channel = %v, want %q", reqBody["channel"], tt.channel)
					}
					if reqBody["ts"] != tt.ts {
						t.Errorf("ts = %v, want %q", reqBody["ts"], tt.ts)
					}

					// Create response
					respBody, _ := json.Marshal(tt.response)
					return &http.Response{
						StatusCode: tt.httpStatus,
						Body:       io.NopCloser(strings.NewReader(string(respBody))),
						Header:     make(http.Header),
					}, nil
				},
			}

			client := &Client{
				botToken: testutil.FakeSlackBotToken,
				httpClient: &http.Client{
					Transport: transport,
					Timeout:   30 * time.Second,
				},
			}

			ctx := context.Background()
			err := client.UpdateMessage(ctx, tt.channel, tt.ts, tt.msg)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestClientPostMessageNetworkError tests network-level errors
func TestClientPostMessageNetworkError(t *testing.T) {
	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network error: connection refused")
		},
	}

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}

	ctx := context.Background()
	msg := &Message{Channel: "#test", Text: "test"}
	_, err := client.PostMessage(ctx, msg)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to post message") {
		t.Errorf("error = %q, want to contain 'failed to post message'", err.Error())
	}
}

// TestClientUpdateMessageNetworkError tests network-level errors for update
func TestClientUpdateMessageNetworkError(t *testing.T) {
	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network error: connection refused")
		},
	}

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}

	ctx := context.Background()
	msg := &Message{Text: "test"}
	err := client.UpdateMessage(ctx, "C123", "123.456", msg)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to update message") {
		t.Errorf("error = %q, want to contain 'failed to update message'", err.Error())
	}
}

// TestClientPostMessageInvalidJSON tests handling of invalid JSON response
func TestClientPostMessageInvalidJSON(t *testing.T) {
	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not valid json")),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}

	ctx := context.Background()
	msg := &Message{Channel: "#test", Text: "test"}
	_, err := client.PostMessage(ctx, msg)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error = %q, want to contain 'failed to parse response'", err.Error())
	}
}

// TestClientUpdateMessageInvalidJSON tests handling of invalid JSON response for update
func TestClientUpdateMessageInvalidJSON(t *testing.T) {
	transport := &mockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not valid json")),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}

	ctx := context.Background()
	msg := &Message{Text: "test"}
	err := client.UpdateMessage(ctx, "C123", "123.456", msg)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error = %q, want to contain 'failed to parse response'", err.Error())
	}
}

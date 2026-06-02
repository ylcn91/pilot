package slack

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createMockSlackServer creates a test server that captures and validates Slack API requests
func createMockSlackServer(t *testing.T, validateMsg func(*Message)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
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

		// Parse body if validator provided
		if validateMsg != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}

			var msg Message
			if err := json.Unmarshal(body, &msg); err != nil {
				t.Fatalf("failed to parse message: %v", err)
			}
			validateMsg(&msg)
		}

		// Return success response
		response := PostMessageResponse{
			OK:      true,
			TS:      "1234567890.123456",
			Channel: "C1234567890",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
}

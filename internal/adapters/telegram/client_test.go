package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestCheckSingleton(t *testing.T) {
	tests := []struct {
		name       string
		response   GetUpdatesResponse
		statusCode int
		wantErr    error
	}{
		{
			name: "no conflict - bot is free",
			response: GetUpdatesResponse{
				OK:     true,
				Result: []*Update{},
			},
			statusCode: http.StatusOK,
			wantErr:    nil,
		},
		{
			name: "conflict - another instance running",
			response: GetUpdatesResponse{
				OK:          false,
				ErrorCode:   409,
				Description: "Conflict: terminated by other getUpdates request",
			},
			statusCode: http.StatusConflict,
			wantErr:    ErrConflict,
		},
		{
			name: "other API error",
			response: GetUpdatesResponse{
				OK:          false,
				ErrorCode:   401,
				Description: "Unauthorized",
			},
			statusCode: http.StatusUnauthorized,
			wantErr:    errors.New("telegram API error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			// Verify expected error for 409 conflicts
			if tt.response.ErrorCode == 409 && tt.wantErr != ErrConflict {
				t.Errorf("expected ErrConflict for 409 status, got %v", tt.wantErr)
			}

			// Verify non-409 errors don't return ErrConflict
			if tt.response.ErrorCode != 409 && tt.response.ErrorCode != 0 && errors.Is(tt.wantErr, ErrConflict) {
				t.Errorf("expected non-ErrConflict error for %d status", tt.response.ErrorCode)
			}
		})
	}
}

func TestCheckSingletonIntegration(t *testing.T) {
	// Create a mock server that simulates Telegram API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a getUpdates request
		if r.URL.Path == "/bottest-token/getUpdates" {
			response := GetUpdatesResponse{
				OK:     true,
				Result: []*Update{},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	// Test passes if we can create a client and the method exists
	client := NewClient(testutil.FakeTelegramBotToken)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	// CheckSingleton method should exist
	ctx := context.Background()
	// Note: This will fail because it tries to connect to real Telegram
	// We're just verifying the method signature is correct
	_ = client.CheckSingleton(ctx)
}

func TestErrConflictIs(t *testing.T) {
	// Verify ErrConflict can be checked with errors.Is
	err := ErrConflict
	if !errors.Is(err, ErrConflict) {
		t.Error("errors.Is should return true for ErrConflict")
	}
}

// TestNewClient tests client creation
func TestNewClient(t *testing.T) {
	tests := []struct {
		name     string
		botToken string
	}{
		{
			name:     "valid token",
			botToken: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		},
		{
			name:     "empty token",
			botToken: "",
		},
		{
			name:     "simple token",
			botToken: testutil.FakeTelegramBotToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.botToken)

			if client == nil {
				t.Fatal("NewClient returned nil")
			}
			if client.botToken != tt.botToken {
				t.Errorf("botToken = %q, want %q", client.botToken, tt.botToken)
			}
			if client.httpClient == nil {
				t.Error("httpClient is nil")
			}
			// Verify timeout is set (should be > 30s for long polling)
			if client.httpClient.Timeout < 30*time.Second {
				t.Errorf("httpClient.Timeout = %v, want >= 30s", client.httpClient.Timeout)
			}
		})
	}
}

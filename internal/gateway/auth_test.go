package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewAuthenticator(t *testing.T) {
	config := &AuthConfig{
		Type:  AuthTypeAPIToken,
		Token: testutil.FakeBearerToken,
	}

	auth := NewAuthenticator(config)

	if auth == nil {
		t.Fatal("NewAuthenticator returned nil")
	}
	if auth.config != config {
		t.Error("Config not set correctly")
	}
}

func TestAuthenticateAPIToken(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		configToken string
		authHeader  string
		expectError bool
	}{
		{
			name:        "valid token",
			token:       "secret-token-123",
			configToken: "secret-token-123",
			authHeader:  "Bearer secret-token-123",
			expectError: false,
		},
		{
			name:        "invalid token",
			token:       "wrong-token",
			configToken: "secret-token-123",
			authHeader:  "Bearer wrong-token",
			expectError: true,
		},
		{
			name:        "missing authorization header",
			token:       "",
			configToken: "secret-token-123",
			authHeader:  "",
			expectError: true,
		},
		{
			name:        "missing Bearer prefix",
			token:       "secret-token-123",
			configToken: "secret-token-123",
			authHeader:  "secret-token-123",
			expectError: true,
		},
		{
			name:        "lowercase bearer",
			token:       "secret-token-123",
			configToken: "secret-token-123",
			authHeader:  "bearer secret-token-123",
			expectError: false, // Should work with case-insensitive comparison
		},
		{
			name:        "empty token after Bearer",
			token:       "",
			configToken: "secret-token-123",
			authHeader:  "Bearer ",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AuthConfig{
				Type:  AuthTypeAPIToken,
				Token: tt.configToken,
			}
			auth := NewAuthenticator(config)

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			err := auth.Authenticate(req)
			if (err != nil) != tt.expectError {
				t.Errorf("Authenticate() error = %v, expectError = %v", err, tt.expectError)
			}
		})
	}
}

func TestAuthenticateClaudeCode(t *testing.T) {
	tests := []struct {
		name        string
		remoteAddr  string
		expectError bool
	}{
		{
			name:        "localhost 127.0.0.1",
			remoteAddr:  "127.0.0.1:12345",
			expectError: false,
		},
		{
			name:        "localhost with port only",
			remoteAddr:  "localhost:8080",
			expectError: false,
		},
		{
			name:        "IPv6 localhost",
			remoteAddr:  "[::1]:8080",
			expectError: false,
		},
		{
			name:        "external IP",
			remoteAddr:  "192.168.1.100:8080",
			expectError: true,
		},
		{
			name:        "public IP",
			remoteAddr:  "8.8.8.8:443",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AuthConfig{
				Type: AuthTypeClaudeCode,
			}
			auth := NewAuthenticator(config)

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.RemoteAddr = tt.remoteAddr

			err := auth.Authenticate(req)
			if (err != nil) != tt.expectError {
				t.Errorf("Authenticate() error = %v, expectError = %v", err, tt.expectError)
			}
		})
	}
}

func TestAuthenticateUnknownType(t *testing.T) {
	config := &AuthConfig{
		Type: "unknown-type",
	}
	auth := NewAuthenticator(config)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	err := auth.Authenticate(req)

	if err == nil {
		t.Error("Expected error for unknown auth type")
	}
}

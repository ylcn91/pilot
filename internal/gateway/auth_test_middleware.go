package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		authConfig     *AuthConfig
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "nil authenticator allows request",
			authConfig:     nil,
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
		{
			name: "valid token allows request",
			authConfig: &AuthConfig{
				Type:  AuthTypeAPIToken,
				Token: testutil.FakeBearerToken,
			},
			authHeader:     "Bearer " + testutil.FakeBearerToken,
			expectedStatus: http.StatusOK,
		},
		{
			name: "invalid token returns 401",
			authConfig: &AuthConfig{
				Type:  AuthTypeAPIToken,
				Token: testutil.FakeBearerToken,
			},
			authHeader:     "Bearer wrong-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "missing token returns 401",
			authConfig: &AuthConfig{
				Type:  AuthTypeAPIToken,
				Token: testutil.FakeBearerToken,
			},
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "claude-code auth allows localhost",
			authConfig: &AuthConfig{
				Type: AuthTypeClaudeCode,
			},
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var auth *Authenticator
			if tt.authConfig != nil {
				auth = NewAuthenticator(tt.authConfig)
			}

			// Create test handler
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			// Wrap with middleware
			handler := auth.Middleware(testHandler)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			// Set localhost for claude-code auth test
			if tt.authConfig != nil && tt.authConfig.Type == AuthTypeClaudeCode {
				req.RemoteAddr = "127.0.0.1:12345"
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Middleware() status = %d, want %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestAuthMiddleware_NilAuth(t *testing.T) {
	// Test that nil authenticator passes through requests
	var auth *Authenticator

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(testHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for nil auth, got %d", w.Code)
	}
}

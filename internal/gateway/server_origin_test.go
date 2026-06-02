package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOrigin(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{
			name:     "empty origin (same-origin request)",
			origin:   "",
			expected: true,
		},
		{
			name:     "localhost HTTP",
			origin:   "http://localhost",
			expected: true,
		},
		{
			name:     "localhost with port HTTP",
			origin:   "http://localhost:3000",
			expected: true,
		},
		{
			name:     "localhost HTTPS",
			origin:   "https://localhost",
			expected: true,
		},
		{
			name:     "localhost with port HTTPS",
			origin:   "https://localhost:8080",
			expected: true,
		},
		{
			name:     "127.0.0.1 HTTP",
			origin:   "http://127.0.0.1",
			expected: true,
		},
		{
			name:     "127.0.0.1 with port HTTP",
			origin:   "http://127.0.0.1:9000",
			expected: true,
		},
		{
			name:     "127.0.0.1 HTTPS",
			origin:   "https://127.0.0.1",
			expected: true,
		},
		{
			name:     "127.0.0.1 with port HTTPS",
			origin:   "https://127.0.0.1:443",
			expected: true,
		},
		{
			name:     "external origin rejected",
			origin:   "https://example.com",
			expected: false,
		},
		{
			name:     "malicious site rejected",
			origin:   "https://evil-site.com",
			expected: false,
		},
		{
			name:     "HTTP external origin rejected",
			origin:   "http://attacker.com",
			expected: false,
		},
		{
			name:     "localhost subdomain rejected",
			origin:   "https://localhost.evil.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			result := server.upgrader.CheckOrigin(req)
			if result != tt.expected {
				t.Errorf("CheckOrigin(%q) = %v, want %v", tt.origin, result, tt.expected)
			}
		})
	}
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		// Valid localhost origins
		{"http localhost", "http://localhost", true},
		{"http localhost with port", "http://localhost:3000", true},
		{"http localhost with standard port", "http://localhost:80", true},
		{"https localhost", "https://localhost", true},
		{"https localhost with port", "https://localhost:443", true},
		{"http 127.0.0.1", "http://127.0.0.1", true},
		{"http 127.0.0.1 with port", "http://127.0.0.1:8080", true},
		{"https 127.0.0.1", "https://127.0.0.1", true},
		{"https 127.0.0.1 with port", "https://127.0.0.1:9000", true},

		// Invalid/malicious origins
		{"localhost subdomain attack", "https://localhost.evil.com", false},
		{"localhost path attack", "http://localhostevil.com", false},
		{"127.0.0.1 subdomain attack", "https://127.0.0.1.evil.com", false},
		{"external https", "https://example.com", false},
		{"external http", "http://attacker.com", false},
		{"empty string", "", false},
		{"just localhost word", "localhost", false},
		{"localhost with path no protocol", "localhost:3000", false},
		{"file protocol", "file://localhost", false},
		{"ftp protocol", "ftp://localhost", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLocalhost(tt.origin)
			if result != tt.expected {
				t.Errorf("isLocalhost(%q) = %v, want %v", tt.origin, result, tt.expected)
			}
		})
	}
}

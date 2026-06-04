package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebSocketEndpointsRequireAuth guards the fix for the unauthenticated
// control-plane WebSocket: /ws and /ws/dashboard must reject requests that lack
// a valid token when api-token auth is configured, accepting the token from
// either the Authorization header or the query string (browsers cannot set the
// header on a WS handshake). A non-401 status means the auth gate let the
// request through (the subsequent upgrade/store check then fails harmlessly in
// this unit context).
func TestWebSocketEndpointsRequireAuth(t *testing.T) {
	const token = "secret-ws-token"
	server := NewServerWithAuth(
		&Config{Host: "127.0.0.1", Port: 0},
		&AuthConfig{Type: AuthTypeAPIToken, Token: token},
	)

	cases := []struct {
		name       string
		path       string
		query      string
		header     string
		wantUnauth bool
	}{
		{"control no token", "/ws", "", "", true},
		{"control wrong query token", "/ws", "token=nope", "", true},
		{"control valid query token", "/ws", "token=" + token, "", false},
		{"control valid header token", "/ws", "", "Bearer " + token, false},
		{"control valid access_token param", "/ws", "access_token=" + token, "", false},
		{"dashboard no token", "/ws/dashboard", "", "", true},
		{"dashboard wrong query token", "/ws/dashboard", "token=nope", "", true},
		{"dashboard valid query token", "/ws/dashboard", "token=" + token, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := tc.path
			if tc.query != "" {
				url += "?" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			switch tc.path {
			case "/ws":
				server.handleWebSocket(rec, req)
			case "/ws/dashboard":
				server.handleDashboardWebSocket(rec, req)
			}

			gotUnauth := rec.Code == http.StatusUnauthorized
			if gotUnauth != tc.wantUnauth {
				t.Errorf("status = %d, wantUnauthorized = %v", rec.Code, tc.wantUnauth)
			}
		})
	}
}

// TestWebSocketAuthDisabledAllowsConnect confirms that with no auth configured
// (nil authenticator) the WS gate stays open, preserving the local/no-auth
// default — the gate must not 401 when auth is off.
func TestWebSocketAuthDisabledAllowsConnect(t *testing.T) {
	server := NewServerWithAuth(&Config{Host: "127.0.0.1", Port: 0}, nil)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	server.handleWebSocket(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("auth disabled but /ws returned 401")
	}
}

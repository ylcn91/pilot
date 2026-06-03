package gateway

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// --- Gap 1: handleLinearWebhook Ed25519 verification (TASK-295) ---

// newServerWithLinearKey builds a Server whose linearWebhookPublicKey is set,
// exercising the verification branch in handleLinearWebhook. NewServer copies
// Config.LinearWebhookPublicKey into the struct field, so configuring it here
// is enough to flip on verification.
func newServerWithLinearKey(pub ed25519.PublicKey) *Server {
	return NewServer(&Config{
		Host:                   "127.0.0.1",
		Port:                   9090,
		LinearWebhookPublicKey: pub,
	})
}

func TestHandleLinearWebhook_Ed25519Verification(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}

	body := []byte(`{"action":"create","type":"Issue","data":{"id":"abc"}}`)
	validSig := hex.EncodeToString(ed25519.Sign(priv, body))

	// A signature produced by an unrelated key — valid hex, wrong signer.
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey (other): %v", err)
	}
	wrongSig := hex.EncodeToString(ed25519.Sign(otherPriv, body))

	tests := []struct {
		name           string
		signature      string
		expectedStatus int
	}{
		{
			name:           "valid signature passes",
			signature:      validSig,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing signature rejected 401",
			signature:      "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "wrong-key signature rejected 401",
			signature:      wrongSig,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "malformed hex signature rejected 401",
			signature:      "not-hex-zz",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newServerWithLinearKey(pub)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(string(body)))
			if tt.signature != "" {
				req.Header.Set("linear-signature", tt.signature)
			}
			w := httptest.NewRecorder()

			server.handleLinearWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandleLinearWebhook_TamperedBodyRejected signs body A and submits body B
// under the same key — verification must reject (401) even though both are
// valid JSON and the signature is valid hex.
func TestHandleLinearWebhook_TamperedBodyRejected(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}

	signed := []byte(`{"action":"create","type":"Issue","data":{"id":"abc"}}`)
	tampered := `{"action":"create","type":"Issue","data":{"id":"xyz"}}`
	sig := hex.EncodeToString(ed25519.Sign(priv, signed))

	server := newServerWithLinearKey(pub)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(tampered))
	req.Header.Set("linear-signature", sig)
	w := httptest.NewRecorder()

	server.handleLinearWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for tampered body, got %d", w.Code)
	}
}

// TestHandleLinearWebhook_NoKeyConfiguredSkipsVerification confirms that when
// linearWebhookPublicKey is nil, an unsigned request is accepted (200) — the
// dev/migration-friendly path that logs a WARN but does not reject. This is the
// negative control for the verification branch above.
func TestHandleLinearWebhook_NoKeyConfiguredSkipsVerification(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	body := `{"action":"create","type":"Issue","data":{"id":"abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(body))
	// No linear-signature header at all.
	w := httptest.NewRecorder()

	server.handleLinearWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no public key configured, got %d", w.Code)
	}
}

// TestHandleLinearWebhook_ValidSignatureRoutesPayload asserts that a verified
// request is routed to the registered Linear handler with the decoded payload.
func TestHandleLinearWebhook_ValidSignatureRoutesPayload(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}

	body := []byte(`{"action":"create","type":"Issue","data":{"id":"abc"}}`)
	sig := hex.EncodeToString(ed25519.Sign(priv, body))

	server := newServerWithLinearKey(pub)
	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("linear", func(payload map[string]interface{}) {
		captured = payload
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(string(body)))
	req.Header.Set("linear-signature", sig)
	w := httptest.NewRecorder()

	server.handleLinearWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("verified webhook was not routed to the registered handler")
	}
	if got := captured["type"]; got != "Issue" {
		t.Errorf("routed payload type = %v, want Issue", got)
	}
}

// --- Gap 2: handleGithubWebhook HMAC rejection path ---

// githubSig computes a GitHub-style HMAC-SHA256 signature header value for body.
func githubSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newServerWithGithubSecret(secret string) *Server {
	return NewServer(&Config{
		Host:                "127.0.0.1",
		Port:                9090,
		GithubWebhookSecret: secret,
	})
}

func TestHandleGithubWebhook_SignatureValidation(t *testing.T) {
	secret := testutil.FakeWebhookSecret
	body := []byte(`{"action":"opened","issue":{"number":1}}`)

	tests := []struct {
		name           string
		signature      string
		expectedStatus int
	}{
		{
			name:           "valid HMAC signature passes",
			signature:      githubSig(secret, body),
			expectedStatus: http.StatusOK,
		},
		{
			name:           "wrong signature rejected 401",
			signature:      githubSig("a-different-secret", body),
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "missing signature rejected 401",
			signature:      "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "malformed signature (no sha256 prefix) rejected 401",
			signature:      "deadbeef",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newServerWithGithubSecret(secret)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(string(body)))
			req.Header.Set("X-GitHub-Event", "issues")
			if tt.signature != "" {
				req.Header.Set("X-Hub-Signature-256", tt.signature)
			}
			w := httptest.NewRecorder()

			server.handleGithubWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandleGithubWebhook_NoSecretSkipsValidation is the negative control: with
// no secret configured the gateway does not call VerifyWebhookSignature, so an
// unsigned request is accepted (200).
func TestHandleGithubWebhook_NoSecretSkipsValidation(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	body := `{"action":"opened","issue":{"number":1}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()

	server.handleGithubWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when no secret configured, got %d", w.Code)
	}
}

// --- Gap 3: bitbucket / azuredevops / gitlab / plane accept+reject coverage ---

func TestHandleBitbucketWebhook(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		payload        string
		expectedStatus int
	}{
		{"GET not allowed", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"PUT not allowed", http.MethodPut, "", http.StatusMethodNotAllowed},
		{"valid POST", http.MethodPost, `{"repository":{"name":"r"}}`, http.StatusOK},
		{"invalid JSON", http.MethodPost, "not json", http.StatusBadRequest},
		{"empty body", http.MethodPost, "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			req := httptest.NewRequest(tt.method, "/webhooks/bitbucket", strings.NewReader(tt.payload))
			w := httptest.NewRecorder()

			server.handleBitbucketWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandleBitbucketWebhook_PropagatesMetadata confirms event key, signature
// and the verbatim raw body are stashed into the routed payload (the raw body
// is needed for downstream HMAC verification).
func TestHandleBitbucketWebhook_PropagatesMetadata(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("bitbucket", func(payload map[string]interface{}) {
		captured = payload
	})

	rawBody := `{ "repository":  {"name":"r"} }`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitbucket", strings.NewReader(rawBody))
	req.Header.Set("X-Event-Key", "repo:push")
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")
	w := httptest.NewRecorder()

	server.handleBitbucketWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_event_type"]; got != "repo:push" {
		t.Errorf("_event_type = %v, want repo:push", got)
	}
	if got := captured["_signature"]; got != "sha256=deadbeef" {
		t.Errorf("_signature = %v, want propagated header", got)
	}
	if got := captured["_raw_body"]; got != rawBody {
		t.Errorf("_raw_body = %q, want exact request body %q", got, rawBody)
	}
}

func TestHandleAzureDevOpsWebhook(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		payload        string
		expectedStatus int
	}{
		{"GET not allowed", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"DELETE not allowed", http.MethodDelete, "", http.StatusMethodNotAllowed},
		{"valid POST", http.MethodPost, `{"eventType":"workitem.created"}`, http.StatusOK},
		{"invalid JSON", http.MethodPost, "{not json", http.StatusBadRequest},
		{"empty body", http.MethodPost, "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			req := httptest.NewRequest(tt.method, "/webhooks/azuredevops", strings.NewReader(tt.payload))
			w := httptest.NewRecorder()

			server.handleAzureDevOpsWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandleAzureDevOpsWebhook_BasicAuthSecret confirms the basic-auth
// credentials are folded into payload["_secret"] as "user:pass".
func TestHandleAzureDevOpsWebhook_BasicAuthSecret(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("azuredevops", func(payload map[string]interface{}) {
		captured = payload
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/azuredevops", strings.NewReader(`{"eventType":"workitem.created"}`))
	req.SetBasicAuth("hookuser", "hookpass")
	w := httptest.NewRecorder()

	server.handleAzureDevOpsWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_secret"]; got != "hookuser:hookpass" {
		t.Errorf("_secret = %v, want hookuser:hookpass", got)
	}
}

func TestHandleGitlabWebhook(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		payload        string
		expectedStatus int
	}{
		{"GET not allowed", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"PATCH not allowed", http.MethodPatch, "", http.StatusMethodNotAllowed},
		{"valid POST", http.MethodPost, `{"object_kind":"issue"}`, http.StatusOK},
		{"invalid JSON", http.MethodPost, "nope", http.StatusBadRequest},
		{"empty body", http.MethodPost, "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			req := httptest.NewRequest(tt.method, "/webhooks/gitlab", strings.NewReader(tt.payload))
			w := httptest.NewRecorder()

			server.handleGitlabWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandleGitlabWebhook_PropagatesHeaders confirms the event type and token
// headers are forwarded on the routed payload.
func TestHandleGitlabWebhook_PropagatesHeaders(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("gitlab", func(payload map[string]interface{}) {
		captured = payload
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", strings.NewReader(`{"object_kind":"issue"}`))
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	req.Header.Set("X-Gitlab-Token", testutil.FakeWebhookSecret)
	w := httptest.NewRecorder()

	server.handleGitlabWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_event_type"]; got != "Issue Hook" {
		t.Errorf("_event_type = %v, want Issue Hook", got)
	}
	if got := captured["_token"]; got != testutil.FakeWebhookSecret {
		t.Errorf("_token = %v, want propagated token", got)
	}
}

func TestHandlePlaneWebhook(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		payload        string
		expectedStatus int
	}{
		{"GET not allowed", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"PUT not allowed", http.MethodPut, "", http.StatusMethodNotAllowed},
		{"valid POST", http.MethodPost, `{"event":"issue","action":"created"}`, http.StatusOK},
		{"invalid JSON", http.MethodPost, "garbage", http.StatusBadRequest},
		{"empty body", http.MethodPost, "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
			req := httptest.NewRequest(tt.method, "/webhooks/plane", strings.NewReader(tt.payload))
			w := httptest.NewRecorder()

			server.handlePlaneWebhook(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestHandlePlaneWebhook_PropagatesMetadata confirms event/signature/delivery
// headers and the verbatim raw body land on the routed payload.
func TestHandlePlaneWebhook_PropagatesMetadata(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})
	var captured map[string]interface{}
	server.Router().RegisterWebhookHandler("plane", func(payload map[string]interface{}) {
		captured = payload
	})

	rawBody := `{ "event":  "issue", "action":"created" }`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/plane", strings.NewReader(rawBody))
	req.Header.Set("X-Plane-Event", "issue")
	req.Header.Set("X-Plane-Signature", "sha256=cafe")
	req.Header.Set("X-Plane-Delivery", "delivery-123")
	w := httptest.NewRecorder()

	server.handlePlaneWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if captured == nil {
		t.Fatal("webhook was not routed to the registered handler")
	}
	if got := captured["_event_type"]; got != "issue" {
		t.Errorf("_event_type = %v, want issue", got)
	}
	if got := captured["_signature"]; got != "sha256=cafe" {
		t.Errorf("_signature = %v, want propagated header", got)
	}
	if got := captured["_delivery_id"]; got != "delivery-123" {
		t.Errorf("_delivery_id = %v, want delivery-123", got)
	}
	if got := captured["_raw_body"]; got != rawBody {
		t.Errorf("_raw_body = %q, want exact request body %q", got, rawBody)
	}
}

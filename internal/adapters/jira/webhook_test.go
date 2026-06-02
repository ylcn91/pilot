package jira

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestNewWebhookHandler(t *testing.T) {
	client := NewClient("https://jira.example.com", "user", "token", PlatformCloud)
	handler := NewWebhookHandler(client, "secret", "pilot")

	if handler == nil {
		t.Fatal("NewWebhookHandler returned nil")
	}
	if handler.pilotLabel != "pilot" {
		t.Errorf("handler.pilotLabel = %s, want 'pilot'", handler.pilotLabel)
	}
}

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"webhookEvent":"jira:issue_created"}`)

	t.Run("valid HMAC signature", func(t *testing.T) {
		secret := testutil.FakeWebhookSecret
		sig := computeJiraHMAC(secret, payload)
		h := NewWebhookHandler(nil, secret, "pilot")
		if !h.VerifySignature(payload, sig) {
			t.Error("expected valid signature to pass")
		}
	})

	t.Run("wrong signature", func(t *testing.T) {
		h := NewWebhookHandler(nil, testutil.FakeWebhookSecret, "pilot")
		if h.VerifySignature(payload, "badhex") {
			t.Error("expected wrong signature to fail")
		}
	})

	t.Run("empty secret fail-closed", func(t *testing.T) {
		h := NewWebhookHandler(nil, "", "pilot")
		if h.VerifySignature(payload, "anything") {
			t.Error("expected empty secret to fail closed without dev flag")
		}
	})

	t.Run("empty secret allowed with dev flag", func(t *testing.T) {
		t.Setenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS", "1")
		h := NewWebhookHandler(nil, "", "pilot")
		if !h.VerifySignature(payload, "anything") {
			t.Error("expected empty secret to pass when PILOT_ALLOW_UNSIGNED_WEBHOOKS=1")
		}
	})
}

// computeJiraHMAC computes the expected HMAC-SHA256 signature for testing.
func computeJiraHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		payload   string
		signature string
		want      bool
		setEnv    bool
	}{
		{
			name:      "valid signature",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      true,
		},
		{
			name:      "invalid signature",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: "sha256=invalid123456789",
			want:      false,
		},
		{
			name:      "empty secret fail-closed",
			secret:    "",
			payload:   `{"action":"opened"}`,
			signature: "anything",
			want:      false,
		},
		{
			name:      "empty secret allowed with dev flag",
			secret:    "",
			payload:   `{"action":"opened"}`,
			signature: "anything",
			want:      true,
			setEnv:    true,
		},
		{
			name:      "missing sha256 prefix",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: "abc123",
			want:      false,
		},
		{
			name:      "wrong payload",
			secret:    "mysecret",
			payload:   `{"action":"closed"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      false,
		},
		{
			name:      "wrong secret",
			secret:    "wrongsecret",
			payload:   `{"action":"opened"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS", "1")
			}
			h := NewWebhookHandler(nil, tt.secret, "pilot")
			got := h.VerifySignature([]byte(tt.payload), tt.signature)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// computeHMAC computes the HMAC-SHA256 signature for testing
func computeHMAC(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignatureStandalone(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		payload   string
		signature string
		want      bool
		setEnv    bool
	}{
		{
			name:      "valid signature",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      true,
		},
		{
			name:      "invalid signature",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: "sha256=invalid123456789",
			want:      false,
		},
		{
			name:      "empty secret fail-closed",
			secret:    "",
			payload:   `{"action":"opened"}`,
			signature: "anything",
			want:      false,
		},
		{
			name:      "empty secret allowed with dev flag",
			secret:    "",
			payload:   `{"action":"opened"}`,
			signature: "anything",
			want:      true,
			setEnv:    true,
		},
		{
			name:      "missing sha256 prefix",
			secret:    "mysecret",
			payload:   `{"action":"opened"}`,
			signature: "abc123",
			want:      false,
		},
		{
			name:      "wrong payload",
			secret:    "mysecret",
			payload:   `{"action":"closed"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      false,
		},
		{
			name:      "wrong secret",
			secret:    "wrongsecret",
			payload:   `{"action":"opened"}`,
			signature: computeHMAC("mysecret", `{"action":"opened"}`),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("PILOT_ALLOW_UNSIGNED_WEBHOOKS", "1")
			}
			got := VerifyWebhookSignature([]byte(tt.payload), tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("VerifyWebhookSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

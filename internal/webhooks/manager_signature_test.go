package webhooks

import (
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

func TestManager_Sign(t *testing.T) {
	manager := NewManager(nil, nil)

	payload := []byte(`{"type":"task.completed"}`)

	// With secret
	sig := manager.sign(payload, testutil.FakeWebhookSecret)
	if sig == "" {
		t.Error("expected signature, got empty string")
	}
	if len(sig) < 10 || sig[:7] != "sha256=" {
		t.Errorf("invalid signature format: %s", sig)
	}

	// Without secret
	sig = manager.sign(payload, "")
	if sig != "" {
		t.Error("expected empty signature without secret")
	}
}

func TestVerifySignature(t *testing.T) {
	manager := NewManager(nil, nil)
	secret := testutil.FakeWebhookSecret
	payload := []byte(`{"type":"task.completed"}`)

	// Generate signature
	sig := manager.sign(payload, secret)

	// Valid signature
	if !VerifySignature(payload, sig, secret) {
		t.Error("expected valid signature verification")
	}

	// Invalid signature
	if VerifySignature(payload, "sha256=invalid", secret) {
		t.Error("expected invalid signature verification")
	}

	// Modified payload
	if VerifySignature([]byte(`{"type":"task.failed"}`), sig, secret) {
		t.Error("expected verification to fail for modified payload")
	}

	// Empty secret
	if VerifySignature(payload, sig, "") {
		t.Error("expected verification to fail with empty secret")
	}
}

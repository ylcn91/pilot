package plane

import (
	"testing"
)

func TestVerifySignature_Valid(t *testing.T) {
	secret := "test-webhook-secret"
	payload := []byte(`{"event":"issue","action":"created"}`)
	sig := computeSignature(secret, payload)

	if !VerifySignature(secret, payload, sig) {
		t.Error("expected valid signature to pass")
	}
}

func TestVerifySignature_Invalid(t *testing.T) {
	secret := "test-webhook-secret"
	payload := []byte(`{"event":"issue","action":"created"}`)

	if VerifySignature(secret, payload, "invalid-signature") {
		t.Error("expected invalid signature to fail")
	}
}

func TestVerifySignature_EmptySecret(t *testing.T) {
	payload := []byte(`{"event":"issue","action":"created"}`)
	if !VerifySignature("", payload, "anything") {
		t.Error("expected empty secret to pass (dev mode)")
	}
}

func TestVerifySignature_WrongPayload(t *testing.T) {
	secret := "test-webhook-secret"
	payload := []byte(`{"event":"issue","action":"created"}`)
	sig := computeSignature(secret, payload)

	tampered := []byte(`{"event":"issue","action":"deleted"}`)
	if VerifySignature(secret, tampered, sig) {
		t.Error("expected tampered payload to fail verification")
	}
}

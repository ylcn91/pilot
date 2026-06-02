package linear

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

// --- VerifyLinearSignature tests (TASK-295) ---

// testKeypair returns a fresh Ed25519 keypair for each test. Using a per-test
// keypair (instead of a hardcoded fixture) avoids any chance of accidentally
// using a real-world Linear public key in source — and ensures the tests
// remain stable across crypto library version changes.
func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return pub, priv
}

func TestVerifyLinearSignature_Valid(t *testing.T) {
	pub, priv := testKeypair(t)
	body := []byte(`{"action":"create","type":"Issue","data":{"id":"abc"}}`)
	sig := ed25519.Sign(priv, body)
	sigHex := hex.EncodeToString(sig)

	if err := VerifyLinearSignature(pub, sigHex, body); err != nil {
		t.Errorf("VerifyLinearSignature returned %v on a valid signature, want nil", err)
	}
}

func TestVerifyLinearSignature_InvalidSignature(t *testing.T) {
	pub, priv := testKeypair(t)
	body := []byte(`{"action":"create","type":"Issue"}`)
	sig := ed25519.Sign(priv, body)
	// Tamper the signature: flip the first byte.
	sig[0] ^= 0xFF
	sigHex := hex.EncodeToString(sig)

	err := VerifyLinearSignature(pub, sigHex, body)
	if !errors.Is(err, ErrLinearSignatureMismatch) {
		t.Errorf("VerifyLinearSignature err = %v, want %v", err, ErrLinearSignatureMismatch)
	}
}

func TestVerifyLinearSignature_MissingHeader(t *testing.T) {
	pub, _ := testKeypair(t)
	body := []byte(`{}`)

	err := VerifyLinearSignature(pub, "", body)
	if !errors.Is(err, ErrLinearMissingSignature) {
		t.Errorf("VerifyLinearSignature err = %v, want %v", err, ErrLinearMissingSignature)
	}
}

func TestVerifyLinearSignature_TamperedBody(t *testing.T) {
	pub, priv := testKeypair(t)
	original := []byte(`{"action":"create","type":"Issue","data":{"id":"abc"}}`)
	tampered := []byte(`{"action":"create","type":"Issue","data":{"id":"xyz"}}`)
	sig := ed25519.Sign(priv, original)
	sigHex := hex.EncodeToString(sig)

	// Sign body A, verify against body B → must reject.
	err := VerifyLinearSignature(pub, sigHex, tampered)
	if !errors.Is(err, ErrLinearSignatureMismatch) {
		t.Errorf("VerifyLinearSignature err = %v, want %v", err, ErrLinearSignatureMismatch)
	}
}

func TestVerifyLinearSignature_NoPublicKeyConfigured(t *testing.T) {
	body := []byte(`{}`)
	// Empty public key → caller must decide whether to log-and-allow or reject.
	err := VerifyLinearSignature(nil, "deadbeef", body)
	if !errors.Is(err, ErrLinearNoPublicKey) {
		t.Errorf("VerifyLinearSignature err = %v, want %v", err, ErrLinearNoPublicKey)
	}
}

func TestVerifyLinearSignature_MalformedHex(t *testing.T) {
	pub, _ := testKeypair(t)
	body := []byte(`{}`)
	err := VerifyLinearSignature(pub, "not-hex-at-all-zz", body)
	if !errors.Is(err, ErrLinearInvalidSignatureEncoding) {
		t.Errorf("VerifyLinearSignature err = %v, want %v", err, ErrLinearInvalidSignatureEncoding)
	}
}

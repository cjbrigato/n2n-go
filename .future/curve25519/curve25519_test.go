package curve25519

import (
	"bytes"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	private, public, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair error: %v", err)
	}
	if len(private) != 32 {
		t.Fatalf("expected private key length 32, got %d", len(private))
	}
	if len(public) != 32 {
		t.Fatalf("expected public key length 32, got %d", len(public))
	}
}

func TestComputeSharedSecret(t *testing.T) {
	// Generate two key pairs.
	privA, pubA, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair A error: %v", err)
	}
	privB, pubB, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair B error: %v", err)
	}
	// Compute shared secrets.
	secretA, err := ComputeSharedSecret(privA, pubB)
	if err != nil {
		t.Fatalf("ComputeSharedSecret A error: %v", err)
	}
	secretB, err := ComputeSharedSecret(privB, pubA)
	if err != nil {
		t.Fatalf("ComputeSharedSecret B error: %v", err)
	}
	// Both shared secrets must be equal.
	if !bytes.Equal(secretA, secretB) {
		t.Fatalf("shared secrets do not match: %x vs %x", secretA, secretB)
	}
}

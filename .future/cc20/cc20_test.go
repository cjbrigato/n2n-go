package cc20

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestChacha20EncryptionDecryption(t *testing.T) {
	key := make([]byte, KeySize)
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("failed to generate nonce: %v", err)
	}
	plaintext := []byte("Hello, ChaCha20!")
	ciphertext, err := Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	decrypted, err := Decrypt(ciphertext, key, nonce)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

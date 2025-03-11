package aes

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestCBCEncryptionDecryption(t *testing.T) {
	// Generate a random 16-byte key (AES-128) and IV.
	key := make([]byte, 16)
	iv := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("The quick brown fox jumps over the lazy dog")

	// Encrypt the plaintext.
	ciphertext, err := EncryptCBC(plaintext, key, iv)
	if err != nil {
		t.Fatalf("EncryptCBC error: %v", err)
	}

	// Decrypt the ciphertext.
	decrypted, err := DecryptCBC(ciphertext, key, iv)
	if err != nil {
		t.Fatalf("DecryptCBC error: %v", err)
	}

	// Verify that the decrypted text matches the original plaintext.
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted text does not match original, got %q", decrypted)
	}
}

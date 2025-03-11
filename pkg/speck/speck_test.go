package speck

import (
	"bytes"
	"testing"
)

func TestSpeckBlock(t *testing.T) {
	key := []byte("thisis16bytekey") // 16 bytes
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}
	plaintext := []byte("8bytes!!") // 8 bytes block
	ciphertext := make([]byte, 8)
	decbuf := make([]byte, 8)
	c.Encrypt(ciphertext, plaintext)
	c.Decrypt(decbuf, ciphertext)
	if !bytes.Equal(plaintext, decbuf) {
		t.Errorf("decryption failed: expected %q, got %q", plaintext, decbuf)
	}
}

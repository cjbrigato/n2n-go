package pearson

import (
	"testing"
)

func TestHashEmpty(t *testing.T) {
	if h := Hash([]byte{}); h != 0 {
		t.Errorf("Expected hash of empty slice to be 0, got %d", h)
	}
}

func TestHashConsistency(t *testing.T) {
	data := []byte("The quick brown fox jumps over the lazy dog")
	h1 := Hash(data)
	h2 := Hash(data)
	if h1 != h2 {
		t.Errorf("Hash is inconsistent: %d vs %d", h1, h2)
	}
}

func TestHash64(t *testing.T) {
	data := []byte("Pearson hashing in Go!")
	h64 := Hash64(data)
	// Just check that we get a non-zero 64-bit hash.
	if h64 == 0 {
		t.Errorf("Expected non-zero 64-bit hash, got %d", h64)
	}
}

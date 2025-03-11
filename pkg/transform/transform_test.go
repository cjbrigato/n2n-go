package transform

import (
	"bytes"
	//"n2n-go/pkg/speck"
	"testing"
)

func TestSpeckTransformer(t *testing.T) {
	// 16-byte key and 8-byte IV for Speck.
	key := []byte("thisis16bytekey")
	iv := []byte("8byteiv!")
	// Create Speck cipher.
	//speckCipher, err := speck.NewCipher(key)
	/*if err != nil {
		t.Fatalf("speck.NewCipher error: %v", err)
	}*/
	tr, err := NewTransformer("speck", key, iv)
	if err != nil {
		t.Fatalf("NewTransformer(speck) error: %v", err)
	}
	data := []byte("Data for Speck transform, needs padding!")
	transformed, err := tr.Transform(data)
	if err != nil {
		t.Fatalf("Speck Transform error: %v", err)
	}
	inv, err := tr.InverseTransform(transformed)
	if err != nil {
		t.Fatalf("Speck InverseTransform error: %v", err)
	}
	if !bytes.Equal(data, inv) {
		t.Fatalf("Speck transform round-trip failed, expected %q, got %q", data, inv)
	}
}

func TestTFTransformer(t *testing.T) {
	key := []byte("xorkey")
	tr, err := NewTransformer("tf", key, nil)
	if err != nil {
		t.Fatalf("NewTransformer(tf) error: %v", err)
	}
	data := []byte("Test data for TF transform")
	transformed, err := tr.Transform(data)
	if err != nil {
		t.Fatalf("TF Transform error: %v", err)
	}
	inv, err := tr.InverseTransform(transformed)
	if err != nil {
		t.Fatalf("TF InverseTransform error: %v", err)
	}
	if !bytes.Equal(data, inv) {
		t.Fatalf("TF transform round-trip failed, expected %q, got %q", data, inv)
	}
}

// Similar tests can be written for LZO and Zstd transformers.

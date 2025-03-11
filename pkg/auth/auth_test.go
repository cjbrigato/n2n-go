package auth

import (
	"bytes"
	"testing"
)

func TestBinToASCIIAndBack(t *testing.T) {
	original := []byte("Hello, world!")
	hexStr := BinToASCII(original)
	decoded, err := ASCIIToBin(hexStr)
	if err != nil {
		t.Fatalf("ASCIIToBin failed: %v", err)
	}
	if !bytes.Equal(original, decoded) {
		t.Fatalf("Expected %v, got %v", original, decoded)
	}
}

func TestGeneratePrivateKey(t *testing.T) {
	key, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("Expected key length 32, got %d", len(key))
	}
}

func TestGeneratePublicKey(t *testing.T) {
	priv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey failed: %v", err)
	}
	pub, err := GeneratePublicKey(priv)
	if err != nil {
		t.Fatalf("GeneratePublicKey failed: %v", err)
	}
	if len(pub) != 32 {
		t.Fatalf("Expected public key length 32, got %d", len(pub))
	}
}

func TestGenerateSharedSecret(t *testing.T) {
	privA, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey failed: %v", err)
	}
	pubA, err := GeneratePublicKey(privA)
	if err != nil {
		t.Fatalf("GeneratePublicKey failed: %v", err)
	}
	privB, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey failed: %v", err)
	}
	pubB, err := GeneratePublicKey(privB)
	if err != nil {
		t.Fatalf("GeneratePublicKey failed: %v", err)
	}
	secretA, err := GenerateSharedSecret(privA, pubB)
	if err != nil {
		t.Fatalf("GenerateSharedSecret failed: %v", err)
	}
	secretB, err := GenerateSharedSecret(privB, pubA)
	if err != nil {
		t.Fatalf("GenerateSharedSecret failed: %v", err)
	}
	if !bytes.Equal(secretA, secretB) {
		t.Fatalf("Shared secrets do not match: %x vs %x", secretA, secretB)
	}
}

func TestBindPrivateKeyToUsername(t *testing.T) {
	priv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey failed: %v", err)
	}
	boundKey1, err := BindPrivateKeyToUsername(priv, "alice")
	if err != nil {
		t.Fatalf("BindPrivateKeyToUsername failed: %v", err)
	}
	boundKey2, err := BindPrivateKeyToUsername(priv, "bob")
	if err != nil {
		t.Fatalf("BindPrivateKeyToUsername failed: %v", err)
	}
	if bytes.Equal(boundKey1, boundKey2) {
		t.Fatal("Bound keys for different usernames should differ")
	}
}

func TestCalculateDynamicKey(t *testing.T) {
	key1, err := CalculateDynamicKey(123456, "community1", "federation1")
	if err != nil {
		t.Fatalf("CalculateDynamicKey failed: %v", err)
	}
	if len(key1) != 16 {
		t.Fatalf("Expected dynamic key length 16, got %d", len(key1))
	}
	key2, err := CalculateDynamicKey(123456, "community1", "federation1")
	if err != nil {
		t.Fatalf("CalculateDynamicKey failed: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Fatal("Dynamic key should be consistent for same input")
	}
	key3, err := CalculateDynamicKey(123457, "community1", "federation1")
	if err != nil {
		t.Fatalf("CalculateDynamicKey failed: %v", err)
	}
	if bytes.Equal(key1, key3) {
		t.Fatal("Dynamic key should change for different keyTime")
	}
}

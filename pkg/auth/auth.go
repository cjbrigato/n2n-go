package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"golang.org/x/crypto/curve25519"
)

// BinToASCII converts binary data to its hexadecimal ASCII representation.
func BinToASCII(data []byte) string {
	return hex.EncodeToString(data)
}

// ASCIIToBin converts a hexadecimal ASCII representation back to binary.
func ASCIIToBin(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("invalid hex string")
	}
	return hex.DecodeString(s)
}

// GeneratePrivateKey generates a random 32-byte private key.
func GeneratePrivateKey() ([]byte, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// GeneratePublicKey computes the public key for a given private key using Curve25519.
func GeneratePublicKey(private []byte) ([]byte, error) {
	if len(private) != 32 {
		return nil, errors.New("private key must be 32 bytes")
	}
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	return public, nil
}

// GenerateSharedSecret computes a shared secret using your private key and a peer's public key.
func GenerateSharedSecret(private, peerPublic []byte) ([]byte, error) {
	if len(private) != 32 || len(peerPublic) != 32 {
		return nil, errors.New("both keys must be 32 bytes")
	}
	return curve25519.X25519(private, peerPublic)
}

// BindPrivateKeyToUsername binds a private key to a username by deriving a new key.
// The resulting key is computed as SHA256(privateKey || username) and is 32 bytes long.
func BindPrivateKeyToUsername(private []byte, username string) ([]byte, error) {
	if len(private) != 32 {
		return nil, errors.New("private key must be 32 bytes")
	}
	h := sha256.New()
	h.Write(private)
	h.Write([]byte(username))
	return h.Sum(nil), nil
}

// CalculateDynamicKey calculates a dynamic key based on a key time and community strings.
// It returns a 16-byte dynamic key computed as the first 16 bytes of SHA256(keyTime || comm || fed).
func CalculateDynamicKey(keyTime uint32, comm, fed string) ([]byte, error) {
	h := sha256.New()
	// Convert keyTime to 4 bytes (big-endian)
	var b [4]byte
	b[0] = byte(keyTime >> 24)
	b[1] = byte(keyTime >> 16)
	b[2] = byte(keyTime >> 8)
	b[3] = byte(keyTime)
	h.Write(b[:])
	h.Write([]byte(comm))
	h.Write([]byte(fed))
	fullHash := h.Sum(nil)
	// Return the first 16 bytes as the dynamic key.
	return fullHash[:16], nil
}

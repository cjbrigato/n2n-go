package curve25519

import (
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeyPair generates a Curve25519 key pair.
// It returns a 32-byte private key and a 32-byte public key.
func GenerateKeyPair() (private, public []byte, err error) {
	private = make([]byte, 32)
	if _, err = rand.Read(private); err != nil {
		return nil, nil, err
	}
	// Compute the public key using the standard Basepoint.
	public, err = curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	return private, public, nil
}

// ComputeSharedSecret computes a shared secret using a private key and a peer's public key.
// Both keys must be 32 bytes in length.
func ComputeSharedSecret(private, peerPublic []byte) ([]byte, error) {
	if len(private) != 32 || len(peerPublic) != 32 {
		return nil, errors.New("private and public keys must be 32 bytes")
	}
	secret, err := curve25519.X25519(private, peerPublic)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

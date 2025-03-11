// Package cc20 provides functions to encrypt and decrypt data using ChaCha20.
// It acts as a replacement for the original CC20 implementation.
package cc20

import (
	"errors"

	"golang.org/x/crypto/chacha20"
)

// KeySize defines the key size in bytes for ChaCha20.
const KeySize = chacha20.KeySize

// NonceSize defines the nonce size in bytes for ChaCha20.
const NonceSize = chacha20.NonceSize

// Encrypt encrypts the given plaintext using ChaCha20 with the specified key and nonce.
// The key must be exactly 32 bytes and the nonce must be exactly 12 bytes.
// It returns the ciphertext or an error if the parameters are invalid or the cipher cannot be created.
func Encrypt(plaintext, key, nonce []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, errors.New("invalid key length, must be 32 bytes")
	}
	if len(nonce) != NonceSize {
		return nil, errors.New("invalid nonce length, must be 12 bytes")
	}
	cipher, err := chacha20.NewUnauthenticatedCipher(key, nonce)
	if err != nil {
		return nil, err
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.XORKeyStream(ciphertext, plaintext)
	return ciphertext, nil
}

// Decrypt decrypts the given ciphertext using ChaCha20 with the specified key and nonce.
// Since ChaCha20 is symmetric, decryption is implemented by calling Encrypt.
func Decrypt(ciphertext, key, nonce []byte) ([]byte, error) {
	// Decryption is the same as encryption.
	return Encrypt(ciphertext, key, nonce)
}

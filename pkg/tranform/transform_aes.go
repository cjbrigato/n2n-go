package transform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

func keyFromPassphrase(passphrase string) []byte {
	key := sha256.Sum256([]byte(passphrase))
	return key[:]
}

type aesGCMTransform struct{ gcm cipher.AEAD }

func NewAESGCMTransform(passphrase string) (Transform, error) {
	key := keyFromPassphrase(passphrase)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aesgcm: failed to create cipher block: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aesgcm: failed to create GCM: %w", err)
	}
	return &aesGCMTransform{gcm: gcm}, nil
}
func (e *aesGCMTransform) Apply(plaintext []byte) ([]byte, error) { /* ... encryption ... */
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("aesgcm apply (encrypt): failed to generate nonce: %w", err)
	}
	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}
func (e *aesGCMTransform) Reverse(ciphertext []byte) ([]byte, error) { /* ... decryption ... */
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("aesgcm reverse (decrypt): ciphertext too short")
	}
	nonce, encryptedMessage := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, encryptedMessage, nil)
	if err != nil {
		return nil, fmt.Errorf("aesgcm reverse (decrypt): failed to open GCM message: %w", err)
	}
	return plaintext, nil
}

package headerencryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const BlockSize = 16

// HEContext represents a header encryption context.
// Its Key field must be exactly 16 bytes.
type HEContext struct {
	Key []byte
}

// NewHEContext creates a new HEContext using a 16‑byte key.
func NewHEContext(key []byte) (*HEContext, error) {
	if len(key) != BlockSize {
		return nil, errors.New("key must be 16 bytes")
	}
	// Validate key by attempting to create an AES cipher.
	if _, err := aes.NewCipher(key); err != nil {
		return nil, err
	}
	return &HEContext{Key: key}, nil
}

// EncryptHeader encrypts the header portion of a packet.
// It expects headerLen to be exactly 16 bytes.
// The function embeds the provided stamp (8 bytes) into the plaintext header
// (in the first 8 bytes; the remaining 8 bytes are set to zero),
// encrypts the 16‑byte block using AES‑CBC with the key from ctx and IV from ctxIV,
// and writes the resulting ciphertext back into packet[0:16].
func EncryptHeader(packet []byte, headerLen, packetLen int, ctx, ctxIV *HEContext, stamp uint64) error {
	if headerLen != BlockSize {
		return errors.New("headerLen must be 16 bytes")
	}
	if len(packet) < headerLen {
		return errors.New("packet too short for header encryption")
	}

	// Prepare plaintext header:
	plaintext := make([]byte, BlockSize)
	// Embed the stamp into the first 8 bytes.
	binary.BigEndian.PutUint64(plaintext[:8], stamp)
	// For simplicity, bytes 8-15 are zero.

	// Create AES cipher with ctx.Key.
	block, err := aes.NewCipher(ctx.Key)
	if err != nil {
		return err
	}
	// Use IV from ctxIV.Key.
	if len(ctxIV.Key) != BlockSize {
		return errors.New("ctxIV key must be 16 bytes")
	}
	iv := make([]byte, BlockSize)
	copy(iv, ctxIV.Key)

	// Encrypt using CBC mode.
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, BlockSize)
	mode.CryptBlocks(ciphertext, plaintext)

	// Replace the header in the packet with the ciphertext.
	copy(packet[:BlockSize], ciphertext)
	return nil
}

// DecryptHeader decrypts the header portion of a packet and recovers the stamp.
// It decrypts the first 16 bytes of the packet using AES‑CBC with the key from ctx and IV from ctxIV,
// then extracts the stamp from the first 8 bytes of the decrypted header.
func DecryptHeader(packet []byte, packetLen int, communityName string, ctx, ctxIV *HEContext) (uint64, error) {
	if len(packet) < BlockSize {
		return 0, errors.New("packet too short for header decryption")
	}
	// Create AES cipher with ctx.Key.
	block, err := aes.NewCipher(ctx.Key)
	if err != nil {
		return 0, err
	}
	if len(ctxIV.Key) != BlockSize {
		return 0, errors.New("ctxIV key must be 16 bytes")
	}
	iv := make([]byte, BlockSize)
	copy(iv, ctxIV.Key)

	ciphertext := packet[:BlockSize]
	plaintext := make([]byte, BlockSize)
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	// Extract stamp from the first 8 bytes.
	stamp := binary.BigEndian.Uint64(plaintext[:8])
	// Optionally, copy the decrypted header back into the packet.
	copy(packet[:BlockSize], plaintext)
	return stamp, nil
}

// SetupHeaderEncryptionKey derives header encryption contexts from a community name.
// It returns static and dynamic contexts for encryption and IV.
func SetupHeaderEncryptionKey(communityName string) (ctxStatic, ctxDynamic, ctxIVStatic, ctxIVDynamic *HEContext, err error) {
	derive := func(prefix, community string) []byte {
		h := sha256.New()
		h.Write([]byte(prefix + community))
		sum := h.Sum(nil)
		return sum[:BlockSize]
	}
	keyStatic := derive("he_static:", communityName)
	keyDynamic := derive("he_dynamic:", communityName)
	ivStatic := derive("he_iv_static:", communityName)
	ivDynamic := derive("he_iv_dynamic:", communityName)

	ctxStatic, err = NewHEContext(keyStatic)
	if err != nil {
		return
	}
	ctxDynamic, err = NewHEContext(keyDynamic)
	if err != nil {
		return
	}
	ctxIVStatic, err = NewHEContext(ivStatic)
	if err != nil {
		return
	}
	ctxIVDynamic, err = NewHEContext(ivDynamic)
	if err != nil {
		return
	}
	return
}

// ChangeDynamicHeaderKey updates the dynamic header encryption contexts using a new key.
// keyDynamic must be 16 bytes. The dynamic key is updated and a new IV is derived by hashing.
func ChangeDynamicHeaderKey(keyDynamic []byte, ctxDynamic, ctxIVDynamic *HEContext) error {
	if len(keyDynamic) != BlockSize {
		return errors.New("keyDynamic must be 16 bytes")
	}
	// Update dynamic encryption key.
	ctxDynamic.Key = make([]byte, BlockSize)
	copy(ctxDynamic.Key, keyDynamic)

	// Derive a new dynamic IV from the new key.
	h := sha256.New()
	h.Write([]byte("he_iv_dynamic_new"))
	h.Write(keyDynamic)
	newIV := h.Sum(nil)[:BlockSize]
	ctxIVDynamic.Key = make([]byte, BlockSize)
	copy(ctxIVDynamic.Key, newIV)
	return nil
}

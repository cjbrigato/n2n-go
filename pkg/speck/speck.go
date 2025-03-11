// Package speck implements the Speck64/128 block cipher.
// Speck64/128 has a block size of 8 bytes (64 bits) and a key size of 16 bytes (128 bits)
// with 32 rounds.
package speck

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

const (
	blockSize  = 8  // bytes
	keyWords   = 4  // 16 bytes key => 4 uint32 words
	numRounds  = 32 // fixed number of rounds for Speck64/128
)

// Cipher represents a Speck64/128 cipher.
type Cipher struct {
	roundKeys [numRounds]uint32
}

// NewCipher creates a new Speck64/128 cipher with the given 16-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyWords*4 {
		return nil, errors.New("speck: invalid key size, must be 16 bytes")
	}
	var K [keyWords]uint32
	for i := 0; i < keyWords; i++ {
		K[i] = binary.LittleEndian.Uint32(key[i*4 : (i+1)*4])
	}
	c := &Cipher{}
	// Key schedule: a = K[0], b = [K[1], K[2], K[3]]
	a := K[0]
	var b [keyWords - 1]uint32
	b[0] = K[1]
	b[1] = K[2]
	b[2] = K[3]
	c.roundKeys[0] = a
	for i := 0; i < numRounds-1; i++ {
		// Rotate right a by 8 bits
		a = bits.RotateLeft32(a, -8)
		// a = (a + b[i mod 3]) XOR i
		a = (a + b[i%(keyWords-1)]) ^ uint32(i)
		// Rotate left b[i mod 3] by 3 bits and XOR with new a
		b[i%(keyWords-1)] = bits.RotateLeft32(b[i%(keyWords-1)], 3) ^ a
		c.roundKeys[i+1] = a
	}
	return c, nil
}

// blockSize returns the block size in bytes.
func (c *Cipher) BlockSize() int { return blockSize }

// Encrypt encrypts an 8-byte plaintext block into dst.
// src and dst must be 8 bytes long.
func (c *Cipher) Encrypt(dst, src []byte) {
	if len(src) < blockSize || len(dst) < blockSize {
		panic("speck: input not full block")
	}
	// Load plaintext as two uint32 words (little-endian)
	x := binary.LittleEndian.Uint32(src[0:4])
	y := binary.LittleEndian.Uint32(src[4:8])
	for i := 0; i < numRounds; i++ {
		x = bits.RotateLeft32(x, -8)
		x = (x + y) ^ c.roundKeys[i]
		y = bits.RotateLeft32(y, 3)
		y = y ^ x
	}
	binary.LittleEndian.PutUint32(dst[0:4], x)
	binary.LittleEndian.PutUint32(dst[4:8], y)
}

// Decrypt decrypts an 8-byte ciphertext block from src into dst.
// src and dst must be 8 bytes long.
func (c *Cipher) Decrypt(dst, src []byte) {
	if len(src) < blockSize || len(dst) < blockSize {
		panic("speck: input not full block")
	}
	// Load ciphertext
	x := binary.LittleEndian.Uint32(src[0:4])
	y := binary.LittleEndian.Uint32(src[4:8])
	for i := numRounds - 1; i >= 0; i-- {
		y = y ^ x
		y = bits.RotateLeft32(y, -3)
		x = x ^ c.roundKeys[i]
		x = (x - y) & 0xffffffff // subtraction modulo 2^32
		x = bits.RotateLeft32(x, 8)
	}
	binary.LittleEndian.PutUint32(dst[0:4], x)
	binary.LittleEndian.PutUint32(dst[4:8], y)
}

package aes

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

// EncryptCBC encrypts plaintext using AES in CBC mode with PKCS#7 padding.
// The IV must be exactly one AES block in length.
func EncryptCBC(plaintext, key, iv []byte) ([]byte, error) {
	if len(iv) != aes.BlockSize {
		return nil, errors.New("IV length must equal AES block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)
	return ciphertext, nil
}

// DecryptCBC decrypts ciphertext using AES in CBC mode with PKCS#7 padding.
func DecryptCBC(ciphertext, key, iv []byte) ([]byte, error) {
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext is not a multiple of the block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)
	return pkcs7Unpad(plaintext, aes.BlockSize)
}

// pkcs7Pad pads data using PKCS#7 to a multiple of blockSize.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// pkcs7Unpad removes PKCS#7 padding from data.
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid data size")
	}
	padding := data[len(data)-1]
	if int(padding) > blockSize || int(padding) == 0 {
		return nil, errors.New("invalid padding size")
	}
	// Verify that all padding bytes are equal.
	for i := len(data) - int(padding); i < len(data); i++ {
		if data[i] != padding {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-int(padding)], nil
}

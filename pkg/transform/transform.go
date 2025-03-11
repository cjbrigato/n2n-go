package transform

import (
	"bytes"
	"encoding/binary"
	"errors"

	"n2n-go/pkg/aes"
	"n2n-go/pkg/cc20"
	"n2n-go/pkg/minilzo"
	"n2n-go/pkg/speck"
	"n2n-go/pkg/tf"

	"github.com/klauspost/compress/zstd"
)

// Transformer defines the interface for applying and reverting a transformation.
type Transformer interface {
	Transform(data []byte) ([]byte, error)
	InverseTransform(data []byte) ([]byte, error)
}

// NewTransformer creates a transformer by name.
// key and iv (or nonce) are used when needed.
func NewTransformer(name string, key, iv []byte) (Transformer, error) {
	switch name {
	case "null":
		return &NullTransformer{}, nil
	case "aes":
		if key == nil || iv == nil {
			return nil, errors.New("aes transformer requires key and iv")
		}
		return &AesTransformer{key: key, iv: iv}, nil
	case "cc20":
		if key == nil || iv == nil {
			return nil, errors.New("cc20 transformer requires key and nonce")
		}
		return &Cc20Transformer{key: key, nonce: iv}, nil
	case "speck":
		if key == nil || iv == nil {
			return nil, errors.New("speck transformer requires key and iv")
		}
		// Create Speck cipher; key must be 16 bytes.
		c, err := speck.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return &SpeckTransformer{cipher: c, iv: iv}, nil
	case "tf":
		if key == nil {
			return nil, errors.New("tf transformer requires a key")
		}
		return &TFTransformer{key: key}, nil
	case "lzo":
		// LZO transform: compress then later decompress.
		return &LzoTransformer{}, nil
	case "zstd":
		// Create a zstd encoder and decoder.
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			return nil, err
		}
		dec, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		return &ZstdTransformer{encoder: enc, decoder: dec}, nil
	default:
		return nil, errors.New("unknown transformer: " + name)
	}
}

// NullTransformer is a pass-through transformer.
type NullTransformer struct{}

func (n *NullTransformer) Transform(data []byte) ([]byte, error) {
	return data, nil
}
func (n *NullTransformer) InverseTransform(data []byte) ([]byte, error) {
	return data, nil
}

// AesTransformer wraps AES-CBC encryption.
type AesTransformer struct {
	key, iv []byte
}

func (a *AesTransformer) Transform(data []byte) ([]byte, error) {
	return aes.EncryptCBC(data, a.key, a.iv)
}
func (a *AesTransformer) InverseTransform(data []byte) ([]byte, error) {
	return aes.DecryptCBC(data, a.key, a.iv)
}

// Cc20Transformer wraps ChaCha20 encryption.
type Cc20Transformer struct {
	key, nonce []byte
}

func (c *Cc20Transformer) Transform(data []byte) ([]byte, error) {
	return cc20.Encrypt(data, c.key, c.nonce)
}
func (c *Cc20Transformer) InverseTransform(data []byte) ([]byte, error) {
	return cc20.Decrypt(data, c.key, c.nonce)
}

// SpeckTransformer implements CBC mode for Speck.
// Block size is 8 bytes; uses PKCS#7 padding.
type SpeckTransformer struct {
	cipher *speck.Cipher
	iv     []byte // must be 8 bytes long
}

func (s *SpeckTransformer) Transform(data []byte) ([]byte, error) {
	blockSize := s.cipher.BlockSize()
	// Apply PKCS#7 padding.
	padded := pkcs7Pad(data, blockSize)
	out := make([]byte, len(padded))
	prev := s.iv
	tmp := make([]byte, blockSize)
	for i := 0; i < len(padded); i += blockSize {
		// XOR plaintext block with prev
		for j := 0; j < blockSize; j++ {
			tmp[j] = padded[i+j] ^ prev[j]
		}
		// Encrypt block
		s.cipher.Encrypt(out[i:i+blockSize], tmp)
		// Update prev to current ciphertext block.
		prev = out[i : i+blockSize]
	}
	return out, nil
}

func (s *SpeckTransformer) InverseTransform(data []byte) ([]byte, error) {
	blockSize := s.cipher.BlockSize()
	if len(data)%blockSize != 0 {
		return nil, errors.New("speck: invalid ciphertext length")
	}
	out := make([]byte, len(data))
	prev := s.iv
	tmp := make([]byte, blockSize)
	for i := 0; i < len(data); i += blockSize {
		// Decrypt current block into tmp.
		s.cipher.Decrypt(tmp, data[i:i+blockSize])
		// XOR with prev to get plaintext block.
		for j := 0; j < blockSize; j++ {
			out[i+j] = tmp[j] ^ prev[j]
		}
		prev = data[i : i+blockSize]
	}
	// Remove PKCS#7 padding.
	return pkcs7Unpad(out, blockSize)
}

// TFTransformer applies a simple XOR transformation.
type TFTransformer struct {
	key []byte
}

func (t *TFTransformer) Transform(data []byte) ([]byte, error) {
	return tf.Transform(data, t.key), nil
}
func (t *TFTransformer) InverseTransform(data []byte) ([]byte, error) {
	// Same as transform.
	return tf.Transform(data, t.key), nil
}

// LzoTransformer compresses data with LZO.
// It prefixes the compressed output with a 4-byte big-endian length of the original data.
type LzoTransformer struct{}

func (l *LzoTransformer) Transform(data []byte) ([]byte, error) {
	compressed, err := minilzo.Compress(data)
	if err != nil {
		return nil, err
	}
	// Prepend original length.
	buf := make([]byte, 4+len(compressed))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], compressed)
	return buf, nil
}

func (l *LzoTransformer) InverseTransform(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("lzo: data too short")
	}
	origLen := int(binary.BigEndian.Uint32(data[:4]))
	return minilzo.Decompress(data[4:], origLen)
}

// ZstdTransformer compresses data using Zstandard.
type ZstdTransformer struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func (z *ZstdTransformer) Transform(data []byte) ([]byte, error) {
	// zstd.Encoder writes complete streams that contain length info.
	return z.encoder.EncodeAll(data, nil), nil
}

func (z *ZstdTransformer) InverseTransform(data []byte) ([]byte, error) {
	return z.decoder.DecodeAll(data, nil)
}

// Helper functions for PKCS#7 padding.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	pad := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, pad...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("pkcs7: invalid data length")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize {
		return nil, errors.New("pkcs7: invalid padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, errors.New("pkcs7: invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}

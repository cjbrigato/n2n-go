package minilzo

import (
	"bytes"

	"github.com/rasky/go-lzo"
)

// Compress compresses the input data using the LZO1X algorithm.
// It returns the compressed data. (lzo.Compress1X does not return an error.)
func Compress(data []byte) ([]byte, error) {
	// lzo.Compress1X takes the input slice and returns the compressed bytes.
	compressed := lzo.Compress1X(data)
	return compressed, nil
}

// Decompress decompresses LZO‑compressed data using lzo.Decompress1X.
// The parameter originalSize is the expected size of the uncompressed data.
// It returns the decompressed data or an error if decompression fails.
func Decompress(data []byte, originalSize int) ([]byte, error) {
	// Wrap the compressed data in a bytes.Reader
	reader := bytes.NewReader(data)
	// Call lzo.Decompress1X with the reader, input length, and expected output length.
	decompressed, err := lzo.Decompress1X(reader, len(data), originalSize)
	if err != nil {
		return nil, err
	}
	return decompressed, nil
}

package transform

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd" // Import the library
)

type zstdTransform struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
	level   zstd.EncoderLevel
}

// NewZstdTransform creates a compression/decompression transform using Zstandard.
// Provide a compression level like zstd.SpeedFastest, zstd.SpeedDefault,
// zstd.SpeedBetterCompression, etc.
func NewZstdTransform(level zstd.EncoderLevel) (Transform, error) {
	// Pre-initialize encoder and decoder for potential reuse/pooling.
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(level))
	if err != nil {
		return nil, fmt.Errorf("zstd: failed to initialize encoder: %w", err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("zstd: failed to initialize decoder: %w", err)
	}

	return &zstdTransform{
		encoder: enc,
		decoder: dec,
		level:   level, // Store level for reference or potential re-init
	}, nil
}

// Apply compresses the data using Zstandard.
func (s *zstdTransform) Apply(data []byte) ([]byte, error) {
	// Use Reset to reuse the encoder's internal buffers with a new output writer.
	var buf bytes.Buffer
	s.encoder.Reset(&buf) // Reuse the encoder instance

	_, err := s.encoder.Write(data)
	if err != nil {
		// Even if Write fails, try to close to release resources, ignore close error.
		_ = s.encoder.Close()
		return nil, fmt.Errorf("zstd apply (compress): failed to write data: %w", err)
	}

	// Close is essential to finalize the compressed stream and flush buffers.
	// For pooled encoders, Close() is often called implicitly by Reset or explicitly managed.
	// With the pattern above (Reset then Write), Close flushes the last block.
	err = s.encoder.Close()
	if err != nil {
		return nil, fmt.Errorf("zstd apply (compress): failed to close writer: %w", err)
	}

	return buf.Bytes(), nil
}

// Reverse decompresses the data using Zstandard.
func (s *zstdTransform) Reverse(data []byte) ([]byte, error) {
	// Use Reset to reuse the decoder's internal buffers with a new input reader.
	reader := bytes.NewReader(data)
	err := s.decoder.Reset(reader) // Reuse the decoder instance
	if err != nil {
		// Reset can fail if the dictionary is invalid etc. (not used here)
		return nil, fmt.Errorf("zstd reverse (decompress): failed to reset decoder: %w", err)
	}

	// ReadAll reads until EOF or error. Efficient for buffered readers.
	decompressed, err := io.ReadAll(s.decoder)
	if err != nil {
		return nil, fmt.Errorf("zstd reverse (decompress): failed to read data: %w", err)
	}

	return decompressed, nil

}

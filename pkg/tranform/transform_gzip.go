package transform

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

type gzipTransform struct{}

func NewGzipTransform() Transform { return &gzipTransform{} }
func (g *gzipTransform) Apply(data []byte) ([]byte, error) { /* ... compression ... */
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("gzip apply (compress): failed to write data: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip apply (compress): failed to close writer: %w", err)
	}
	return buf.Bytes(), nil
}
func (g *gzipTransform) Reverse(data []byte) ([]byte, error) { /* ... decompression ... */
	buf := bytes.NewReader(data)
	gz, err := gzip.NewReader(buf)
	if err != nil {
		return nil, fmt.Errorf("gzip reverse (decompress): failed to create reader: %w", err)
	}
	defer gz.Close()
	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("gzip reverse (decompress): failed to read data: %w", err)
	}
	return decompressed, nil
}

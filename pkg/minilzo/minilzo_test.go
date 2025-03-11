package minilzo

import (
	"bytes"
	"testing"
)

func TestCompressDecompress(t *testing.T) {
	original := []byte("This is a test of pure-Go LZO compression. " +
		"It should compress and then decompress back to the original data. " +
		"Minilzo in Go, no cgo required!")

	compressed, err := Compress(original)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Decompress the data; we supply the original length.
	decompressed, err := Decompress(compressed, len(original))
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if !bytes.Equal(original, decompressed) {
		t.Fatalf("Decompressed data does not match original.\nExpected: %q\nGot:      %q", original, decompressed)
	}
}

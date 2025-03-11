package hexdump

import (
	"bytes"
	"strings"
	"testing"
)

func TestFHexDump(t *testing.T) {
	// Example data: a simple message.
	data := []byte("Hello, World! This is a test of hexdump.\n")
	var buf bytes.Buffer

	if err := FHexDump(0, data, &buf); err != nil {
		t.Fatalf("FHexDump error: %v", err)
	}

	output := buf.String()
	// Check that the output contains the expected hex representation for "Hello"
	if !strings.Contains(output, "48 65 6c 6c 6f") {
		t.Errorf("Expected output to contain hex for 'Hello', got:\n%s", output)
	}
	t.Logf("Hexdump output:\n%s", output)
}

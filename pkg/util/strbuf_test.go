package util

import "testing"

func TestStrBuf(t *testing.T) {
	buf := NewStrBuf()
	buf.Write("Hello, ")
	buf.WriteLine("World!")
	buf.Writef("Number: %d", 42)

	result := buf.String()
	expected := "Hello, World!\nNumber: 42"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	buf.Reset()
	if buf.String() != "" {
		t.Errorf("Expected empty buffer after reset, got %q", buf.String())
	}
}

// Package util provides low-level utility functions and types
// that serve as a foundation for higher-level n2n functionality.
package util

import (
	"fmt"
	"strings"
)

// StrBuf is a simple string buffer that mimics the behavior of the C strbuf.
type StrBuf struct {
	builder strings.Builder
}

// NewStrBuf creates and returns a new, empty StrBuf.
func NewStrBuf() *StrBuf {
	return &StrBuf{}
}

// Write appends the given string to the buffer.
func (sb *StrBuf) Write(s string) {
	sb.builder.WriteString(s)
}

// WriteLine appends the given string followed by a newline.
func (sb *StrBuf) WriteLine(s string) {
	sb.builder.WriteString(s)
	sb.builder.WriteString("\n")
}

// Writef appends a formatted string to the buffer.
func (sb *StrBuf) Writef(format string, args ...interface{}) {
	sb.builder.WriteString(fmt.Sprintf(format, args...))
}

// String returns the accumulated string.
func (sb *StrBuf) String() string {
	return sb.builder.String()
}

// Reset clears the buffer.
func (sb *StrBuf) Reset() {
	sb.builder.Reset()
}

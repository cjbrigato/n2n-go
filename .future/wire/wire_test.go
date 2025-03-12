package wire

import (
	"bytes"
	"testing"
	"time"

	"n2n-go/pkg/protocol"
)

// fakeRW is a simple in‑memory implementation of ReadWriteCloser.
type fakeRW struct {
	buf bytes.Buffer
}

func (f *fakeRW) Read(p []byte) (int, error) {
	return f.buf.Read(p)
}

func (f *fakeRW) Write(p []byte) (int, error) {
	return f.buf.Write(p)
}

func (f *fakeRW) Close() error {
	return nil
}

func TestWritePacket(t *testing.T) {
	// Create a fake read/write interface.
	fake := &fakeRW{}

	// Create a new handler using the fake interface.
	handler := NewHandler(fake)

	// Define test parameters.
	community := "n2ncommunity"
	payload := []byte("testpayload")

	// Write a packet.
	err := handler.WritePacket(payload, community)
	if err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}

	// The fake interface now contains the written packet.
	data := fake.buf.Bytes()
	if len(data) < protocol.TotalHeaderSize {
		t.Fatalf("Written packet too short: %d bytes", len(data))
	}

	// Unmarshal the header.
	var hdr protocol.PacketHeader
	err = hdr.UnmarshalBinary(data[:protocol.TotalHeaderSize])
	if err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	// Check header fields.
	if hdr.Version != 3 {
		t.Errorf("Expected version 3, got %d", hdr.Version)
	}
	if hdr.TTL != 64 {
		t.Errorf("Expected TTL 64, got %d", hdr.TTL)
	}
	if hdr.Flags != 0 {
		t.Errorf("Expected Flags 0, got %d", hdr.Flags)
	}
	if hdr.Sequence == 0 {
		t.Errorf("Expected non-zero sequence, got %d", hdr.Sequence)
	}
	// Check community: trim zeros.
	commStr := string(hdr.Community[:])
	if commStr != community && commStr != community+"\x00\x00\x00\x00\x00\x00" { // allow extra zeros
		t.Errorf("Expected community %q, got %q", community, commStr)
	}

	// Check payload.
	extractedPayload := data[protocol.TotalHeaderSize:]
	if string(extractedPayload) != string(payload) {
		t.Errorf("Payload mismatch: expected %q, got %q", payload, extractedPayload)
	}
}

func TestHeaderTimestampVerification(t *testing.T) {
	// Create a header.
	community := "n2ncommunity"
	hdr := protocol.NewPacketHeader(3, 64, 0, 1, community)
	data, err := hdr.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}
	// Unmarshal header.
	var hdr2 protocol.PacketHeader
	if err := hdr2.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}
	// Verify timestamp (should be within a 16-second drift from now).
	if !hdr2.VerifyTimestamp(time.Now(), 16*time.Second) {
		t.Errorf("Timestamp verification failed; header timestamp: %d, now: %d", hdr2.Timestamp, time.Now().UnixNano())
	}
}

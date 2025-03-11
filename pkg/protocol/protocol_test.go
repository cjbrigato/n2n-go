package protocol

import (
	"bytes"
	"testing"
	"time"
)

func TestMarshalUnmarshalHeader(t *testing.T) {
	// Create a header.
	orig := NewPacketHeader(3, 64, 0, 12345, "n2ncommunity")
	// Marshal it.
	data, err := orig.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}
	if len(data) != TotalHeaderSize {
		t.Fatalf("Expected header size %d, got %d", TotalHeaderSize, len(data))
	}

	// Unmarshal into a new header.
	var hdr PacketHeader
	err = hdr.UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	// Compare fields.
	if hdr.Version != orig.Version || hdr.TTL != orig.TTL || hdr.Sequence != orig.Sequence {
		t.Errorf("Header fields mismatch")
	}
	if string(bytes.Trim(hdr.Community[:], "\x00")) != "n2ncommunity" {
		t.Errorf("Community mismatch: got %q", string(hdr.Community[:]))
	}
	// Check checksum field.
	if hdr.Checksum != orig.Checksum {
		t.Errorf("Checksum mismatch: expected %d, got %d", orig.Checksum, hdr.Checksum)
	}
}

func TestVerifyTimestamp(t *testing.T) {
	// Create a header with current time.
	hdr := NewPacketHeader(3, 64, 0, 1, "test")
	data, _ := hdr.MarshalBinary()
	var newHdr PacketHeader
	newHdr.UnmarshalBinary(data)

	ref := time.Now()
	// Allow drift of 2 seconds.
	if !newHdr.VerifyTimestamp(ref, 2*time.Second) {
		t.Errorf("Timestamp should be within drift")
	}

	// Create a header with old timestamp.
	newHdr.Timestamp = ref.Add(-3 * time.Second).UnixNano()
	if newHdr.VerifyTimestamp(ref, 2*time.Second) {
		t.Errorf("Timestamp verification should fail for too-old timestamp")
	}
}

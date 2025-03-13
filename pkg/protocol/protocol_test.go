package protocol

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestMarshalUnmarshalHeader(t *testing.T) {
	// Create a header
	orig := NewHeader(3, 64, TypeRegister, 12345, "n2ncommunity", "edge1", "")

	// Marshal it
	data, err := orig.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	if len(data) != TotalHeaderSize {
		t.Fatalf("Expected header size %d, got %d", TotalHeaderSize, len(data))
	}

	// Unmarshal into a new header
	var hdr Header
	err = hdr.UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	// Compare fields
	if hdr.Version != orig.Version {
		t.Errorf("Version mismatch: expected %d, got %d", orig.Version, hdr.Version)
	}

	if hdr.TTL != orig.TTL {
		t.Errorf("TTL mismatch: expected %d, got %d", orig.TTL, hdr.TTL)
	}

	if hdr.PacketType != orig.PacketType {
		t.Errorf("PacketType mismatch: expected %d, got %d", orig.PacketType, hdr.PacketType)
	}

	if hdr.Sequence != orig.Sequence {
		t.Errorf("Sequence mismatch: expected %d, got %d", orig.Sequence, hdr.Sequence)
	}

	if hdr.GetCommunity() != "n2ncommunity" {
		t.Errorf("Community mismatch: expected %q, got %q", "n2ncommunity", hdr.GetCommunity())
	}

	if hdr.GetSourceID() != "edge1" {
		t.Errorf("SourceID mismatch: expected %q, got %q", "edge1", hdr.GetSourceID())
	}

	// Check checksum field
	if hdr.Checksum != orig.Checksum {
		t.Errorf("Checksum mismatch: expected %d, got %d", orig.Checksum, hdr.Checksum)
	}
}

func TestHeaderGetters(t *testing.T) {
	// Test string getters
	hdr := NewHeader(1, 64, TypeData, 100, "testcommunity", "source123", "dest456")

	if hdr.GetSourceID() != "source123" {
		t.Errorf("GetSourceID returned %q, expected %q", hdr.GetSourceID(), "source123")
	}

	if hdr.GetDestinationID() != "dest456" {
		t.Errorf("GetDestinationID returned %q, expected %q", hdr.GetDestinationID(), "dest456")
	}

	if hdr.GetCommunity() != "testcommunity" {
		t.Errorf("GetCommunity returned %q, expected %q", hdr.GetCommunity(), "testcommunity")
	}
}

func TestHeaderWithDestMAC(t *testing.T) {
	// Test creating a header with MAC address
	mac, err := net.ParseMAC("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}

	hdr := NewHeaderWithDestMAC(1, 64, TypeData, 100, "testcommunity", "source123", mac)

	// Get MAC back and compare
	destMAC := hdr.GetDestinationMAC()
	if destMAC == nil {
		t.Fatalf("GetDestinationMAC returned nil")
	}

	if !bytes.Equal(destMAC, mac) {
		t.Errorf("MAC mismatch: expected %v, got %v", mac, destMAC)
	}

	// Test with empty MAC
	hdr = NewHeader(1, 64, TypeData, 100, "testcommunity", "source123", "")
	if hdr.GetDestinationMAC() != nil {
		t.Errorf("Expected nil MAC for header without MAC")
	}
}

func TestVerifyTimestamp(t *testing.T) {
	// Create a header with current time
	hdr := NewHeader(3, 64, TypeData, 1, "test", "edge1", "")

	// Should pass with default drift
	if !hdr.VerifyTimestamp(time.Now(), DefaultTimestampDrift) {
		t.Errorf("Timestamp verification failed for current time with default drift")
	}

	// Test with small drift (should fail)
	tooSmallDrift := 10 * time.Millisecond
	time.Sleep(20 * time.Millisecond) // Wait a bit to ensure time difference
	if hdr.VerifyTimestamp(time.Now(), tooSmallDrift) {
		t.Errorf("Timestamp verification should fail with too small drift")
	}

	// Test with future timestamp
	futureHdr := NewHeader(3, 64, TypeData, 1, "test", "edge1", "")
	futureHdr.Timestamp = time.Now().Add(5 * time.Second).UnixNano()
	if !futureHdr.VerifyTimestamp(time.Now(), 10*time.Second) {
		t.Errorf("Timestamp verification failed for future timestamp within drift")
	}

	if futureHdr.VerifyTimestamp(time.Now(), 3*time.Second) {
		t.Errorf("Timestamp verification should fail for future timestamp outside drift")
	}
}

func TestPacketTypeString(t *testing.T) {
	testCases := []struct {
		packetType PacketType
		expected   string
	}{
		{TypeRegister, "Register"},
		{TypeUnregister, "Unregister"},
		{TypeHeartbeat, "Heartbeat"},
		{TypeData, "Data"},
		{TypeAck, "Ack"},
		{PacketType(100), "Unknown"}, // Unknown type
	}

	for _, tc := range testCases {
		if tc.packetType.String() != tc.expected {
			t.Errorf("PacketType(%d).String() = %q, expected %q",
				tc.packetType, tc.packetType.String(), tc.expected)
		}
	}
}

func TestHeaderUnmarshalWithInsufficientData(t *testing.T) {
	// Test with too short data
	shortData := make([]byte, TotalHeaderSize-1)
	var hdr Header
	err := hdr.UnmarshalBinary(shortData)

	if err == nil {
		t.Error("Expected error for insufficient data, got nil")
	}

	if err != ErrInsufficientData {
		t.Errorf("Expected ErrInsufficientData, got %v", err)
	}
}

func TestHeaderChecksumVerification(t *testing.T) {
	// Only run this test if checksums are enabled
	if NoChecksum {
		t.Skip("Skipping checksum test since NoChecksum=true")
	}

	// Create and marshal a header
	orig := NewHeader(3, 64, TypeData, 1, "test", "edge1", "")
	data, err := orig.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Tamper with the payload but not the checksum
	data[21] ^= 0xFF // Flip bits in the source ID

	// Unmarshal should detect the tampering
	var hdr Header
	err = hdr.UnmarshalBinary(data)

	if err == nil {
		t.Error("Expected checksum verification error, got nil")
	}

	if err != ErrChecksumVerification {
		t.Errorf("Expected ErrChecksumVerification, got %v", err)
	}
}

func BenchmarkMarshalBinary(b *testing.B) {
	hdr := NewHeader(3, 64, TypeData, 12345, "testcommunity", "source123", "dest456")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hdr.MarshalBinary()
	}
}

func BenchmarkUnmarshalBinary(b *testing.B) {
	hdr := NewHeader(3, 64, TypeData, 12345, "testcommunity", "source123", "dest456")
	data, _ := hdr.MarshalBinary()

	var h Header
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.UnmarshalBinary(data)
	}
}

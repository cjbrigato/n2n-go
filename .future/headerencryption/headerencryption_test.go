package headerencryption

import (
	"encoding/binary"
	"testing"
)

func TestHeaderEncryptionDecryption(t *testing.T) {
	// Use a test community name.
	community := "testCommunity"
	ctxStatic, _, ctxIVStatic, _, err := SetupHeaderEncryptionKey(community)
	if err != nil {
		t.Fatalf("SetupHeaderEncryptionKey failed: %v", err)
	}

	// Create a dummy packet (at least 16 bytes).
	packet := make([]byte, 32)
	// Define a stamp.
	var stamp uint64 = 0x1122334455667788

	// Encrypt the header (first 16 bytes).
	err = EncryptHeader(packet, 16, len(packet), ctxStatic, ctxIVStatic, stamp)
	if err != nil {
		t.Fatalf("EncryptHeader failed: %v", err)
	}

	// For testing, decrypt the header.
	recoveredStamp, err := DecryptHeader(packet, len(packet), community, ctxStatic, ctxIVStatic)
	if err != nil {
		t.Fatalf("DecryptHeader failed: %v", err)
	}

	if recoveredStamp != stamp {
		t.Fatalf("Recovered stamp mismatch: expected %x, got %x", stamp, recoveredStamp)
	}
}

func TestChangeDynamicHeaderKey(t *testing.T) {
	community := "testCommunity"
	_, ctxDynamic, _, ctxIVDynamic, err := SetupHeaderEncryptionKey(community)
	if err != nil {
		t.Fatalf("SetupHeaderEncryptionKey failed: %v", err)
	}

	newKey := make([]byte, 16)
	for i := 0; i < 16; i++ {
		newKey[i] = byte(i + 1)
	}
	err = ChangeDynamicHeaderKey(newKey, ctxDynamic, ctxIVDynamic)
	if err != nil {
		t.Fatalf("ChangeDynamicHeaderKey failed: %v", err)
	}
	// Verify that the new dynamic key is set.
	if len(ctxDynamic.Key) != 16 {
		t.Fatalf("Dynamic key length is not 16")
	}
	// Optionally, print part of the new IV for inspection.
	newStamp := binary.BigEndian.Uint64(ctxIVDynamic.Key[:8])
	t.Logf("New dynamic IV stamp: %x", newStamp)
}

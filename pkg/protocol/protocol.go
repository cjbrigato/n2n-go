// Package protocol implements a simplified protocol framing and utility functions
// for n2n. It defines a packet header structure (with common fields such as version,
// TTL, sequence, timestamp, checksum, and community), and functions to marshal/unmarshal
// headers as well as compute a checksum for integrity and replay protection.
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"

	"n2n-go/pkg/pearson"
)

const (
	// HeaderFixedSize is the fixed size in bytes of our header (without the variable-length community field).
	// Fields: Version(1) + TTL(1) + Flags(1) + Sequence(2) + Timestamp(8) + Checksum(8) = 21 bytes.
	HeaderFixedSize = 21

	// CommunitySize is the fixed size of the community field.
	CommunitySize = 20

	// TotalHeaderSize is the total size of the header.
	TotalHeaderSize = HeaderFixedSize + CommunitySize
)

// PacketHeader represents the common header fields for a packet.
type PacketHeader struct {
	Version   uint8               // Protocol version
	TTL       uint8               // Time-to-live
	Flags     uint8               // Flags (e.g., encryption enabled, heartbeat, etc.)
	Sequence  uint16              // Sequence number
	Timestamp int64               // UnixNano timestamp
	Checksum  uint64              // Checksum over header (with the checksum field zeroed)
	Community [CommunitySize]byte // Community name (padded to 20 bytes)
}

// NewPacketHeader creates a new PacketHeader with given parameters and current timestamp.
func NewPacketHeader(version, ttl, flags uint8, seq uint16, community string) *PacketHeader {
	var comm [CommunitySize]byte
	copy(comm[:], []byte(community))
	return &PacketHeader{
		Version:   version,
		TTL:       ttl,
		Flags:     flags,
		Sequence:  seq,
		Timestamp: time.Now().UnixNano(),
		Community: comm,
	}
}

// MarshalBinary serializes the PacketHeader into a fixed-size byte slice.
// The checksum field is computed over all header bytes (with the checksum field zeroed out).
func (h *PacketHeader) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write fields in order.
	if err := binary.Write(buf, binary.BigEndian, h.Version); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.TTL); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Flags); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Sequence); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Timestamp); err != nil {
		return nil, err
	}
	// Reserve space for checksum (write zero for now).
	if err := binary.Write(buf, binary.BigEndian, uint64(0)); err != nil {
		return nil, err
	}
	// Write community field.
	if _, err := buf.Write(h.Community[:]); err != nil {
		return nil, err
	}

	data := buf.Bytes()
	// Zero out the checksum field for computation.
	// The checksum field should be at offset 13 and occupy 8 bytes.
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	for i := 13; i < 21; i++ {
		dataCopy[i] = 0
	}
	checksum := pearson.Hash64(dataCopy)
	h.Checksum = checksum
	// Now, write the checksum into the correct position.
	binary.BigEndian.PutUint64(data[13:21], checksum)

	return data, nil
}

// UnmarshalBinary deserializes data into a PacketHeader and verifies its checksum.
func (h *PacketHeader) UnmarshalBinary(data []byte) error {
	if len(data) < TotalHeaderSize {
		return errors.New("insufficient data for header")
	}
	buf := bytes.NewReader(data)

	if err := binary.Read(buf, binary.BigEndian, &h.Version); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.TTL); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Flags); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Sequence); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Timestamp); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Checksum); err != nil {
		return err
	}
	if n, err := buf.Read(h.Community[:]); err != nil || n != CommunitySize {
		return errors.New("failed to read community field")
	}

	// Recompute checksum.
	dataCopy := make([]byte, TotalHeaderSize)
	copy(dataCopy, data[:TotalHeaderSize])
	// Zero out checksum field (offset 13 to 21).
	for i := 13; i < 21; i++ {
		dataCopy[i] = 0
	}
	computed := pearson.Hash64(dataCopy)
	if computed != h.Checksum {
		return errors.New("checksum verification failed")
	}
	return nil
}

// VerifyTimestamp checks whether the header's timestamp is within the allowed drift
// compared to the provided reference time.
func (h *PacketHeader) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

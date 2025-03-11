// Package protocol implements a simplified protocol framing and utility functions
// for n2n. It defines a packet header structure (with common fields such as version,
// TTL, flags, sequence number, community, and timestamp), and functions to marshal/unmarshal
// headers as well as compute a checksum for basic integrity and replay protection.
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"

	"n2n-go/pkg/pearson"
)

const (
	// HeaderFixedSize is the fixed size in bytes of our header (without variable-length fields).
	HeaderFixedSize = 1 + 1 + 1 + 2 + 8 + 8 // Version (1) + TTL (1) + Flags (1) + Sequence (2) + Timestamp (8) + Checksum (8)
	// For simplicity, Community is assumed to be fixed 20 bytes (padded with zeros if needed).
	CommunitySize = 20
	// TotalHeaderSize is the total size of the header.
	TotalHeaderSize = HeaderFixedSize + CommunitySize
)

// PacketHeader represents the common header fields for a packet.
type PacketHeader struct {
	Version   uint8               // Protocol version
	TTL       uint8               // Time-to-live
	Flags     uint8               // Flags (e.g., encryption enabled, etc.)
	Sequence  uint16              // Sequence number
	Timestamp int64               // UnixNano timestamp
	Checksum  uint64              // Checksum over header (or header+payload, see below)
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
// The checksum field is computed over all header bytes except the checksum itself.
func (h *PacketHeader) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write fixed fields.
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
	// Reserve space for Checksum (8 bytes, write zero for now).
	if err := binary.Write(buf, binary.BigEndian, uint64(0)); err != nil {
		return nil, err
	}
	// Write Community field.
	if _, err := buf.Write(h.Community[:]); err != nil {
		return nil, err
	}

	// Compute checksum over all bytes except the checksum field itself.
	// Here, we simply compute the Pearson 64-bit hash of the header with the 8 bytes (checksum) set to 0.
	data := buf.Bytes()
	// data layout: [Version(1) TTL(1) Flags(1) Sequence(2) Timestamp(8) Checksum(8) Community(20)]
	// The checksum occupies bytes 1+1+1+2+8 : 13 through 20 (0-indexed, bytes 13 to 20).
	// It is currently all zeros.
	checksum := pearson.Hash64(data)
	// Overwrite the checksum in the appropriate position.
	binary.BigEndian.PutUint64(data[11:19], checksum)
	h.Checksum = checksum

	return data, nil
}

// UnmarshalBinary deserializes a PacketHeader from data.
// It verifies that the checksum is correct.
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

	// Recompute checksum over header.
	dataCopy := make([]byte, TotalHeaderSize)
	copy(dataCopy, data[:TotalHeaderSize])
	// Zero out the checksum field before recomputing.
	for i := 11; i < 19; i++ {
		dataCopy[i] = 0
	}
	computed := pearson.Hash64(dataCopy)
	if computed != h.Checksum {
		return errors.New("checksum verification failed")
	}

	return nil
}

// VerifyTimestamp checks whether the header's timestamp is within the allowed drift (jitter)
// compared to the provided reference time.
func (h *PacketHeader) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

// Package protocol implements the protocol framing for n2n.
// This refined version includes dedicated fields for source and destination IDs,
// a packet type field, and no redundant text in the payload.
package protocol

import (
	"encoding/binary"
	"errors"
	"time"

	"n2n-go/pkg/pearson"
)

// PacketType represents the type of packet.
type PacketType uint8

const (
	// Define packet types.
	TypeRegister   PacketType = 1
	TypeUnregister PacketType = 2
	TypeHeartbeat  PacketType = 3
	TypeData       PacketType = 4
	TypeAck        PacketType = 5
)

// TotalHeaderSize is the fixed size of the refined header (in bytes).
const TotalHeaderSize = 73

// PacketHeader is the refined protocol header.
type PacketHeader struct {
	Version       uint8      // protocol version
	TTL           uint8      // time-to-live
	PacketType    PacketType // packet type (REGISTER, UNREGISTER, etc.)
	Sequence      uint16     // sequence number
	Timestamp     int64      // timestamp (UnixNano)
	Checksum      uint64     // checksum over header (checksum field zeroed during computation)
	SourceID      [16]byte   // sender identifier (padded to 16 bytes)
	DestinationID [16]byte   // destination identifier (padded to 16 bytes, all zero for broadcast)
	Community     [20]byte   // community name (padded to 20 bytes)
}

// NewPacketHeader creates a new PacketHeader using the given parameters.
// It sets the current timestamp automatically.
func NewPacketHeader(version, ttl uint8, pType PacketType, seq uint16, community string, srcID, destID string) *PacketHeader {
	var comm [20]byte
	copy(comm[:], []byte(community))
	var src [16]byte
	copy(src[:], []byte(srcID))
	var dst [16]byte
	copy(dst[:], []byte(destID))
	return &PacketHeader{
		Version:       version,
		TTL:           ttl,
		PacketType:    pType,
		Sequence:      seq,
		Timestamp:     time.Now().UnixNano(),
		Community:     comm,
		SourceID:      src,
		DestinationID: dst,
	}
}

// MarshalBinary serializes the PacketHeader into a fixed-size byte slice.
// It writes each field into a 73-byte slice, computes the checksum (with the
// checksum field zeroed), and writes the checksum.
func (h *PacketHeader) MarshalBinary() ([]byte, error) {
	data := make([]byte, TotalHeaderSize)
	data[0] = h.Version
	data[1] = h.TTL
	data[2] = uint8(h.PacketType)
	binary.BigEndian.PutUint16(data[3:5], h.Sequence)
	binary.BigEndian.PutUint64(data[5:13], uint64(h.Timestamp))
	// Set checksum field (bytes 13-21) to zero for now.
	for i := 13; i < 21; i++ {
		data[i] = 0
	}
	copy(data[21:37], h.SourceID[:])
	copy(data[37:53], h.DestinationID[:])
	copy(data[53:73], h.Community[:])
	// Compute checksum over the entire header with the checksum field zeroed.
	checksum := pearson.Hash64(data)
	h.Checksum = checksum
	binary.BigEndian.PutUint64(data[13:21], checksum)
	return data, nil
}

// UnmarshalBinary deserializes a PacketHeader from data and verifies its checksum.
func (h *PacketHeader) UnmarshalBinary(data []byte) error {
	if len(data) < TotalHeaderSize {
		return errors.New("insufficient data for header")
	}
	h.Version = data[0]
	h.TTL = data[1]
	h.PacketType = PacketType(data[2])
	h.Sequence = binary.BigEndian.Uint16(data[3:5])
	h.Timestamp = int64(binary.BigEndian.Uint64(data[5:13]))
	h.Checksum = binary.BigEndian.Uint64(data[13:21])
	copy(h.SourceID[:], data[21:37])
	copy(h.DestinationID[:], data[37:53])
	copy(h.Community[:], data[53:73])
	// Recompute checksum with the checksum field zeroed.
	temp := make([]byte, TotalHeaderSize)
	copy(temp, data[:TotalHeaderSize])
	for i := 13; i < 21; i++ {
		temp[i] = 0
	}
	if pearson.Hash64(temp) != h.Checksum {
		return errors.New("checksum verification failed")
	}
	return nil
}

// VerifyTimestamp checks if the header's timestamp is within allowed drift.
func (h *PacketHeader) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

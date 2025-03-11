// Package protocol implements the refined n2n protocol framing.
package protocol

import (
	"encoding/binary"
	"errors"
	"time"

	"n2n-go/pkg/pearson"
)

const TotalHeaderSize = 73 // Fixed header size in bytes

// PacketType defines the type of packet.
type PacketType uint8

const (
	TypeRegister   PacketType = 1
	TypeUnregister PacketType = 2
	TypeHeartbeat  PacketType = 3
	TypeData       PacketType = 4
	TypeAck        PacketType = 5
)

// Header represents the refined protocol header.
type Header struct {
	Version       uint8      // protocol version
	TTL           uint8      // time-to-live
	PacketType    PacketType // packet type
	Sequence      uint16     // sequence number
	Timestamp     int64      // UnixNano timestamp
	Checksum      uint64     // checksum computed over header with checksum field zeroed
	SourceID      [16]byte   // sender's ID (padded to 16 bytes)
	DestinationID [16]byte   // destination ID (all-zero for broadcast)
	Community     [20]byte   // community name (padded to 20 bytes)
}

// NewHeader creates a new Header. For broadcast, destID should be empty.
func NewHeader(version, ttl uint8, pType PacketType, seq uint16, community, srcID, destID string) *Header {
	var comm [20]byte
	copy(comm[:], []byte(community))
	var src [16]byte
	copy(src[:], []byte(srcID))
	var dst [16]byte
	copy(dst[:], []byte(destID))
	return &Header{
		Version:       version,
		TTL:           ttl,
		PacketType:    pType,
		Sequence:      seq,
		Timestamp:     time.Now().UnixNano(),
		SourceID:      src,
		DestinationID: dst,
		Community:     comm,
	}
}

// MarshalBinary serializes the header into a fixed-size byte slice.
// The checksum field (bytes 13-21) is computed using a Pearson hash.
func (h *Header) MarshalBinary() ([]byte, error) {
	data := make([]byte, TotalHeaderSize)
	data[0] = h.Version
	data[1] = h.TTL
	data[2] = uint8(h.PacketType)
	binary.BigEndian.PutUint16(data[3:5], h.Sequence)
	binary.BigEndian.PutUint64(data[5:13], uint64(h.Timestamp))
	// Zero out checksum field.
	for i := 13; i < 21; i++ {
		data[i] = 0
	}
	copy(data[21:37], h.SourceID[:])
	copy(data[37:53], h.DestinationID[:])
	copy(data[53:73], h.Community[:])
	checksum := pearson.Hash64(data)
	h.Checksum = checksum
	binary.BigEndian.PutUint64(data[13:21], checksum)
	return data, nil
}

// UnmarshalBinary deserializes the header from a byte slice and verifies the checksum.
func (h *Header) UnmarshalBinary(data []byte) error {
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
	// Recompute checksum.
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
func (h *Header) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

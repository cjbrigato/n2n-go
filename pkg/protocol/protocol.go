// Package protocol implements the refined n2n protocol framing.
// The header now includes dedicated fields for source and destination
// identifiers (with the destination interpreted as a MAC address), a packet type field,
// sequence number, timestamp, and a checksum.
package protocol

import (
	"encoding/binary"
	"errors"
	"net"
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
	Version       uint8      // Protocol version.
	TTL           uint8      // Time-to-live.
	PacketType    PacketType // Type of packet.
	Sequence      uint16     // Sequence number.
	Timestamp     int64      // Timestamp (UnixNano).
	Checksum      uint64     // Checksum computed over header (with checksum field zeroed during computation).
	SourceID      [16]byte   // Sender identifier.
	DestinationID [16]byte   // Destination identifier (interpreted as destination MAC address).
	Community     [20]byte   // Community name.
}

// NewHeader creates a new Header instance. The dest parameter is interpreted
// as a string (for control messages that do not need a MAC address).
func NewHeader(version, ttl uint8, pType PacketType, seq uint16, community, src, dest string) *Header {
	var comm [20]byte
	copy(comm[:], []byte(community))
	var srcID [16]byte
	copy(srcID[:], []byte(src))
	var destID [16]byte
	// For control messages, dest is provided as a string.
	copy(destID[:], []byte(dest))
	return &Header{
		Version:       version,
		TTL:           ttl,
		PacketType:    pType,
		Sequence:      seq,
		Timestamp:     time.Now().UnixNano(),
		SourceID:      srcID,
		DestinationID: destID,
		Community:     comm,
	}
}

// NewHeaderWithDestMAC creates a new Header instance using a destination MAC address.
// The dest parameter is a net.HardwareAddr (expected length 6); its 6 bytes are stored in the
// first 6 bytes of the DestinationID field (the remaining 10 bytes are zeroed).
func NewHeaderWithDestMAC(version, ttl uint8, pType PacketType, seq uint16, community, src string, dest net.HardwareAddr) *Header {
	h := NewHeader(version, ttl, pType, seq, community, src, "")
	if len(dest) == 6 {
		copy(h.DestinationID[0:6], dest)
	}
	return h
}

// MarshalBinary serializes the header into a fixed-size byte slice.
// The checksum field (bytes 13–21) is computed using the Pearson hash.
func (h *Header) MarshalBinary() ([]byte, error) {
	data := make([]byte, TotalHeaderSize)
	data[0] = h.Version
	data[1] = h.TTL
	data[2] = uint8(h.PacketType)
	binary.BigEndian.PutUint16(data[3:5], h.Sequence)
	binary.BigEndian.PutUint64(data[5:13], uint64(h.Timestamp))
	// Zero out checksum field before computing the checksum.
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

// UnmarshalBinary deserializes the header from a byte slice and verifies its checksum.
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
	// Recompute checksum with checksum field zeroed.
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

// VerifyTimestamp checks whether the header's timestamp is within the allowed drift of the provided reference time.
func (h *Header) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

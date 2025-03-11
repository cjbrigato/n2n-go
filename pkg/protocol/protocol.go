package protocol

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
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
	DestinationID [16]byte   // Destination identifier (used for destination MAC address).
	Community     [20]byte   // Community name.
}

// bufferPool is used to reuse buffers for header serialization.
var bufferPool = sync.Pool{
	New: func() interface{} {
		// Preallocate a buffer of TotalHeaderSize bytes.
		return make([]byte, TotalHeaderSize)
	},
}

// NewHeader creates a new Header instance.
func NewHeader(version, ttl uint8, pType PacketType, seq uint16, community, src, dest string) *Header {
	var comm [20]byte
	copy(comm[:], []byte(community))
	var srcID [16]byte
	copy(srcID[:], []byte(src))
	var destID [16]byte
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

// NewHeaderWithDestMAC creates a header where the destination is provided as a MAC address (expected length 6).
// It stores the MAC in the first 6 bytes of DestinationID.
func NewHeaderWithDestMAC(version, ttl uint8, pType PacketType, seq uint16, community, src string, dest net.HardwareAddr) *Header {
	h := NewHeader(version, ttl, pType, seq, community, src, "")
	if len(dest) == 6 {
		copy(h.DestinationID[0:6], dest)
	}
	return h
}

// MarshalBinary serializes the header into a fixed-size byte slice.
// This version reuses a buffer from bufferPool to reduce allocations.
func (h *Header) MarshalBinary() ([]byte, error) {
	buf := bufferPool.Get().([]byte)
	// Fill in fields.
	buf[0] = h.Version
	buf[1] = h.TTL
	buf[2] = uint8(h.PacketType)
	binary.BigEndian.PutUint16(buf[3:5], h.Sequence)
	binary.BigEndian.PutUint64(buf[5:13], uint64(h.Timestamp))
	// Zero out checksum field.
	for i := 13; i < 21; i++ {
		buf[i] = 0
	}
	copy(buf[21:37], h.SourceID[:])
	copy(buf[37:53], h.DestinationID[:])
	copy(buf[53:73], h.Community[:])
	// Compute checksum.
	checksum := pearson.Hash64(buf)
	h.Checksum = checksum
	binary.BigEndian.PutUint64(buf[13:21], checksum)
	// Make a copy to return so the buffer can be reused.
	result := make([]byte, TotalHeaderSize)
	copy(result, buf)
	bufferPool.Put(buf)
	return result, nil
}

// UnmarshalBinary deserializes a header from a byte slice and verifies its checksum.
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
	temp := data[:TotalHeaderSize]
	// Zero out checksum bytes in temp.
	zeroed := make([]byte, TotalHeaderSize)
	copy(zeroed, temp)
	for i := 13; i < 21; i++ {
		zeroed[i] = 0
	}
	if pearson.Hash64(zeroed) != h.Checksum {
		return errors.New("checksum verification failed")
	}
	return nil
}

// VerifyTimestamp checks if the header's timestamp is within the allowed drift.
func (h *Header) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(0, h.Timestamp)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

package protocol

import (
	"encoding/binary"
	"errors"
	"net"
	"time"

	"n2n-go/pkg/buffers"
	"n2n-go/pkg/pearson"
)

const (
	// NoChecksum can be set to true to disable checksums for testing or performance
	NoChecksum = false

	// TotalHeaderSize is the fixed header size in bytes
	TotalHeaderSize = 73

	// DefaultTimestampDrift is the default allowed timestamp drift for packets
	DefaultTimestampDrift = 16 * time.Second
)

// Common errors
var (
	ErrInsufficientData      = errors.New("insufficient data for header")
	ErrChecksumVerification  = errors.New("checksum verification failed")
	ErrTimestampVerification = errors.New("timestamp verification failed")
)

// PacketType defines the type of packet.
type PacketType uint8

const (
	TypeRegister   PacketType = 1
	TypeUnregister PacketType = 2
	TypeHeartbeat  PacketType = 3
	TypeData       PacketType = 4
	TypeAck        PacketType = 5
)

// String returns a human-readable name for the packet type
func (pt PacketType) String() string {
	switch pt {
	case TypeRegister:
		return "Register"
	case TypeUnregister:
		return "Unregister"
	case TypeHeartbeat:
		return "Heartbeat"
	case TypeData:
		return "Data"
	case TypeAck:
		return "Ack"
	default:
		return "Unknown"
	}
}

// Header represents the protocol header.
type Header struct {
	Version       uint8      // Protocol version.
	TTL           uint8      // Time-to-live.
	PacketType    PacketType // Type of packet.
	Sequence      uint16     // Sequence number.
	Timestamp     int64      // Timestamp (UnixNano).
	Checksum      uint64     // Checksum computed over header (with checksum field zeroed during computation).
	SourceID      [16]byte   // Sender identifier.
	DestinationID [16]byte   // Destination identifier.
	Community     [20]byte   // Community name.
}

// headerBufferPool is used to reuse buffers for header serialization.
var headerBufferPool = buffers.HeaderBufferPool

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

// NewHeaderWithDestMAC creates a header where the destination is provided as a MAC address.
// It stores the MAC in the first 6 bytes of DestinationID.
func NewHeaderWithDestMAC(version, ttl uint8, pType PacketType, seq uint16, community, src string, dest net.HardwareAddr) *Header {
	h := NewHeader(version, ttl, pType, seq, community, src, "")
	if len(dest) == 6 {
		copy(h.DestinationID[0:6], dest)
	}
	return h
}

// MarshalBinary serializes the header into a fixed-size byte slice.
func (h *Header) MarshalBinary() ([]byte, error) {
	buf := headerBufferPool.Get()
	defer headerBufferPool.Put(buf)

	if err := h.MarshalBinaryTo(buf); err != nil {
		return nil, err
	}

	// Create a copy to return
	result := make([]byte, TotalHeaderSize)
	copy(result, buf[:TotalHeaderSize])

	return result, nil
}

// MarshalBinaryTo serializes the header into the provided buffer.
// The buffer must be at least TotalHeaderSize bytes long.
func (h *Header) MarshalBinaryTo(buf []byte) error {
	if len(buf) < TotalHeaderSize {
		return ErrInsufficientData
	}

	// Fill in fields
	buf[0] = h.Version
	buf[1] = h.TTL
	buf[2] = uint8(h.PacketType)
	binary.BigEndian.PutUint16(buf[3:5], h.Sequence)
	binary.BigEndian.PutUint64(buf[5:13], uint64(h.Timestamp))

	// Zero out checksum field for checksum calculation
	for i := 13; i < 21; i++ {
		buf[i] = 0
	}

	copy(buf[21:37], h.SourceID[:])
	copy(buf[37:53], h.DestinationID[:])
	copy(buf[53:73], h.Community[:])

	var checksum uint64
	if !NoChecksum {
		// Compute checksum
		checksum = pearson.Hash64(buf[:TotalHeaderSize])
	}

	h.Checksum = checksum
	binary.BigEndian.PutUint64(buf[13:21], checksum)

	return nil
}

// UnmarshalBinary deserializes a header from a byte slice and verifies its checksum.
func (h *Header) UnmarshalBinary(data []byte) error {
	if len(data) < TotalHeaderSize {
		return ErrInsufficientData
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

	if !NoChecksum {
		// Recompute checksum with zeroed checksum field
		checksumBuf := headerBufferPool.Get()
		defer headerBufferPool.Put(checksumBuf)

		// Copy data to temp buffer
		copy(checksumBuf[:TotalHeaderSize], data[:TotalHeaderSize])

		// Zero out checksum field
		for i := 13; i < 21; i++ {
			checksumBuf[i] = 0
		}

		if pearson.Hash64(checksumBuf[:TotalHeaderSize]) != h.Checksum {
			return ErrChecksumVerification
		}
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

// GetSourceID returns the source ID as a string
func (h *Header) GetSourceID() string {
	return trimNullString(h.SourceID[:])
}

// GetDestinationID returns the destination ID as a string
func (h *Header) GetDestinationID() string {
	return trimNullString(h.DestinationID[:])
}

// GetCommunity returns the community name as a string
func (h *Header) GetCommunity() string {
	return trimNullString(h.Community[:])
}

// GetDestinationMAC returns the destination MAC address if present
func (h *Header) GetDestinationMAC() net.HardwareAddr {
	// Check if there is a MAC address in the first 6 bytes
	empty := true
	for i := 0; i < 6; i++ {
		if h.DestinationID[i] != 0 {
			empty = false
			break
		}
	}

	if empty {
		return nil
	}

	mac := make(net.HardwareAddr, 6)
	copy(mac, h.DestinationID[0:6])
	return mac
}

// trimNullString is a zero-allocation way to trim null bytes.
// It's faster than using bytes.TrimRight or strings.TrimRight which allocate.
func trimNullString(b []byte) string {
	// Find the index of the first null byte
	i := 0
	for i < len(b) && b[i] != 0 {
		i++
	}
	return string(b[:i])
}

package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"n2n-go/pkg/protocol/spec"
	"net"
	"time"
)

const (
	ProtoVHeaderSize = 30
	VersionV         = 5
)

// ProtoVHeader is an optimized network protocol header with reduced overhead
type ProtoVHeader struct {
	// Basic packet information (4 bytes)
	Version    uint8           // Protocol version
	TTL        uint8           // Time-to-live
	PacketType spec.PacketType // Packet type (register, data, heartbeat, etc.)
	Flags      PacketFlag      // Flags for various options

	// Identification (6 bytes)
	Sequence    uint16 // Sequence number
	CommunityID uint32 // Hash of community name - 32-bit to reduce collision risk

	// Addressing (12 bytes)
	SourceID [6]byte // Source identifier (Edge TAP MACAddr)
	DestID   [6]byte // Destination identifier (Dest TAP MACAddr)

	// Verification (8 bytes)
	Timestamp uint32 // Unix timestamp (seconds)
	Checksum  uint32 // CRC32 checksum

}

func bytesToMAC(arr [6]byte) net.HardwareAddr {
	empty := true
	for _, b := range arr {
		if b != 0 {
			empty = false
			break
		}
	}

	if empty {
		return nil
	}

	mac := make(net.HardwareAddr, 6)
	copy(mac, arr[:])
	return mac
}

// Implement IHeader interface for ProtoVHeader
func (h *ProtoVHeader) GetVersion() uint8               { return h.Version }
func (h *ProtoVHeader) GetPacketType() spec.PacketType  { return h.PacketType }
func (h *ProtoVHeader) GetSequence() uint16             { return h.Sequence }
func (h *ProtoVHeader) GetSrcMACAddr() net.HardwareAddr { return bytesToMAC(h.SourceID) }
func (h *ProtoVHeader) GetDstMACAddr() net.HardwareAddr { return bytesToMAC(h.DestID) }
func (h *ProtoVHeader) HasSrcMACAddr() bool             { return h.GetSrcMACAddr() != nil }
func (h *ProtoVHeader) HasDstMACAddr() bool             { return h.GetDstMACAddr() != nil }

// NewProtoVHeader creates a new ProtoV header
func NewProtoVHeader(version, ttl uint8, pType spec.PacketType, seq uint16, community string, src, dst net.HardwareAddr) (*ProtoVHeader, error) {
	h := &ProtoVHeader{
		Version:     version,
		TTL:         ttl,
		PacketType:  pType,
		Flags:       0,
		Sequence:    seq,
		CommunityID: HashCommunity(community),
		Timestamp:   uint32(time.Now().Unix()),
	}

	if src == nil {
		return nil, fmt.Errorf("NewProtoVHeader: source macAddr cannot be nil")
	}
	copy(h.SourceID[:], src[:6])
	if dst != nil {
		copy(h.DestID[:], dst[:6])
	}

	return h, nil
}

// MarshalBinary serializes the ProtoV header
func (h *ProtoVHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, ProtoVHeaderSize)

	if err := h.MarshalBinaryTo(buf); err != nil {
		return nil, err
	}

	return buf, nil
}

// MarshalBinaryTo serializes the ProtoV header into the provided buffer
func (h *ProtoVHeader) MarshalBinaryTo(buf []byte) error {
	if len(buf) < ProtoVHeaderSize {
		return errors.New("buffer too small for ProtoV header")
	}

	// Basic info
	buf[0] = h.Version
	buf[1] = h.TTL
	buf[2] = uint8(h.PacketType)
	buf[3] = uint8(h.Flags)

	// Identification
	binary.BigEndian.PutUint16(buf[4:6], h.Sequence)
	binary.BigEndian.PutUint32(buf[6:10], h.CommunityID) // 32-bit community hash

	// Addressing
	copy(buf[10:16], h.SourceID[:])
	copy(buf[16:22], h.DestID[:])

	// Timestamp
	binary.BigEndian.PutUint32(buf[22:26], h.Timestamp)

	// Zero checksum field for calculation
	binary.BigEndian.PutUint32(buf[26:30], 0)

	// Calculate checksum
	h.Checksum = crc32.ChecksumIEEE(buf[:30])
	binary.BigEndian.PutUint32(buf[26:30], h.Checksum)

	return nil
}

// UnmarshalBinary deserializes the ProtoV header
func (h *ProtoVHeader) UnmarshalBinary(data []byte) error {
	if len(data) < ProtoVHeaderSize {
		return errors.New("insufficient data for ProtoV header")
	}

	// Basic info
	h.Version = data[0]
	h.TTL = data[1]
	h.PacketType = spec.PacketType(data[2])
	h.Flags = PacketFlag(data[3])

	// Identification
	h.Sequence = binary.BigEndian.Uint16(data[4:6])
	h.CommunityID = binary.BigEndian.Uint32(data[6:10]) // 32-bit community hash

	// Addressing
	copy(h.SourceID[:], data[10:16])
	copy(h.DestID[:], data[16:22])

	// Timestamp
	h.Timestamp = binary.BigEndian.Uint32(data[22:26])
	h.Checksum = binary.BigEndian.Uint32(data[26:30])

	// Verify checksum
	expectedChecksum := h.Checksum

	// Zero checksum field for calculation
	checksumData := make([]byte, ProtoVHeaderSize)
	copy(checksumData, data[:ProtoVHeaderSize])
	binary.BigEndian.PutUint32(checksumData[26:30], 0)

	actualChecksum := crc32.ChecksumIEEE(checksumData)
	if expectedChecksum != actualChecksum {
		return errors.New("checksum verification failed")
	}

	return nil
}

// VerifyTimestamp checks if the header's timestamp is within the allowed drift
func (h *ProtoVHeader) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(int64(h.Timestamp), 0)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

func (h *ProtoVHeader) SetFlag(flag PacketFlag, value bool) {
	if value {
		h.Flags |= flag
	} else {
		h.Flags &= ^flag
	}
}

func (h *ProtoVHeader) HasFlag(flag PacketFlag) bool {
	return (h.Flags & flag) != 0
}

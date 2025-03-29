package protocol

import (
	"fmt"
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
func NewProtoVHeader(_, ttl uint8, pType spec.PacketType, seq uint16, community string, src, dst net.HardwareAddr) (*ProtoVHeader, error) {
	h := &ProtoVHeader{
		Version:     VersionV,
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

func (h *ProtoVHeader) SetFromSupernode(value bool) {
	h.SetFlag(FlagFromSuperNode, value)
}
func (h *ProtoVHeader) IsFromSupernode() bool {
	return h.HasFlag(FlagFromSuperNode)
}

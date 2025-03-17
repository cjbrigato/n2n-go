package tuntap

import (
	"errors"
	"net"
)

// Common errors returned by functions in this package
var (
	ErrFrameTooShort = errors.New("mac frame too short")
)

// Tagging represents the VLAN tagging type of a MAC frame
type Tagging int

// Tagging constants indicate whether/how a MAC frame is tagged.
// The value represents the number of bytes taken by the tagging.
const (
	NotTagged    Tagging = 0
	Tagged       Tagging = 4 // 802.1Q single tag
	DoubleTagged Tagging = 8 // 802.1ad (Q-in-Q) double tag
)

// Minimum valid lengths and offsets (pre-computed constants)
const (
	MinFrameSize        = 14 // Minimum valid frame size (without FCS)
	SrcOffset           = 6  // Offset to source MAC address
	TypeOffset          = 12 // Offset to type/length field
	MinTaggedFrameSize  = 18 // Minimum size for 802.1Q frame
	TaggedTypeOffset    = 16 // Ethertype position in 802.1Q frame
	MinDoubleTaggedSize = 22 // Minimum size for Q-in-Q frame
	DoubleTypeOffset    = 20 // Ethertype position in Q-in-Q frame
)

// VLAN tag protocol identifiers (as uint16 for faster comparison)
const (
	Dot1QTagType  uint16 = 0x8100 // IEEE 802.1Q VLAN tag
	Dot1AdTagType uint16 = 0x88a8 // IEEE 802.1ad Q-in-Q tag
)

// Well-known MAC address constants
var (
	BroadcastAddr = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

// -----------------------------------------------------------------------------
// Fast path functions (minimal bounds checking, minimal allocations)
// -----------------------------------------------------------------------------

// FastDestination returns the destination MAC address from a frame.
// It assumes the frame is valid and has sufficient length.
// For safety-critical code, use Destination() instead.
func FastDestination(macFrame []byte) net.HardwareAddr {
	return net.HardwareAddr(macFrame[:6])
}

// FastSource returns the source MAC address from a frame.
// It assumes the frame is valid and has sufficient length.
// For safety-critical code, use Source() instead.
func FastSource(macFrame []byte) net.HardwareAddr {
	return net.HardwareAddr(macFrame[6:12])
}

// FastTaggingType returns the tagging type without allocating a new value.
// It assumes the frame is valid and has sufficient length.
func FastTaggingType(macFrame []byte) Tagging {
	// Check for VLAN tag using uint16 comparison for performance
	typeField := uint16(macFrame[12])<<8 | uint16(macFrame[13])

	if typeField == Dot1QTagType {
		// Check for Q-in-Q, but only if the frame is long enough
		if len(macFrame) >= 18 &&
			uint16(macFrame[16])<<8|uint16(macFrame[17]) == Dot1QTagType {
			return DoubleTagged
		}
		return Tagged
	} else if typeField == Dot1AdTagType {
		return DoubleTagged
	}

	return NotTagged
}

// FastEthertype returns the Ethertype field from a frame.
// It assumes the frame has been already validated for sufficient length.
// For safety-critical code, use GetEthertype() instead.
func FastEthertype(macFrame []byte, tagging Tagging) Ethertype {
	pos := 12 + int(tagging)
	return Ethertype{macFrame[pos], macFrame[pos+1]}
}

// FastPayload returns the payload data from a frame.
// It assumes the frame has been already validated for sufficient length.
// For safety-critical code, use Payload() instead.
func FastPayload(macFrame []byte, tagging Tagging) []byte {
	return macFrame[12+int(tagging)+2:]
}

// FastVLANID extracts the VLAN ID from a tagged frame.
// It assumes the frame is a valid tagged frame with sufficient length.
// For safety-critical code, use GetVLANID() instead.
func FastVLANID(macFrame []byte) uint16 {
	// Extract the 12 bits of VLAN ID (last 4 bits of first byte + second byte)
	return (uint16(macFrame[14]&0x0F) << 8) | uint16(macFrame[15])
}

// -----------------------------------------------------------------------------
// Safe path functions (with bounds checking and error handling)
// -----------------------------------------------------------------------------

// Destination returns the destination MAC address from a frame.
func Destination(macFrame []byte) (net.HardwareAddr, error) {
	if len(macFrame) < 6 {
		return nil, ErrFrameTooShort
	}
	return FastDestination(macFrame), nil
}

// Source returns the source MAC address from a frame.
func Source(macFrame []byte) (net.HardwareAddr, error) {
	if len(macFrame) < 12 {
		return nil, ErrFrameTooShort
	}
	return FastSource(macFrame), nil
}

// DetectTagging determines the type of VLAN tagging used in a frame.
func DetectTagging(macFrame []byte) (Tagging, error) {
	if len(macFrame) < MinFrameSize {
		return NotTagged, ErrFrameTooShort
	}

	// Use the fast path implementation once we've checked length
	return FastTaggingType(macFrame), nil
}

// GetEthertype returns the Ethertype field from a frame.
func GetEthertype(macFrame []byte) (Ethertype, error) {
	if len(macFrame) < MinFrameSize {
		return Ethertype{}, ErrFrameTooShort
	}

	tagging := FastTaggingType(macFrame)
	minSize := MinFrameSize

	if tagging == Tagged {
		minSize = MinTaggedFrameSize
	} else if tagging == DoubleTagged {
		minSize = MinDoubleTaggedSize
	}

	if len(macFrame) < minSize {
		return Ethertype{}, ErrFrameTooShort
	}

	return FastEthertype(macFrame, tagging), nil
}

// Payload returns the payload data from a frame.
func Payload(macFrame []byte) ([]byte, error) {
	if len(macFrame) < MinFrameSize {
		return nil, ErrFrameTooShort
	}

	tagging := FastTaggingType(macFrame)
	minSize := MinFrameSize + 2 // +2 for ethertype

	if tagging == Tagged {
		minSize = MinTaggedFrameSize
	} else if tagging == DoubleTagged {
		minSize = MinDoubleTaggedSize
	}

	if len(macFrame) < minSize {
		return nil, ErrFrameTooShort
	}

	return FastPayload(macFrame, tagging), nil
}

// GetVLANID returns the VLAN ID from a tagged frame.
func GetVLANID(macFrame []byte) (uint16, error) {
	if len(macFrame) < MinFrameSize {
		return 0, ErrFrameTooShort
	}

	tagging := FastTaggingType(macFrame)
	if tagging == NotTagged {
		return 0, errors.New("frame is not VLAN tagged")
	}

	if len(macFrame) < MinTaggedFrameSize {
		return 0, ErrFrameTooShort
	}

	return FastVLANID(macFrame), nil
}

// MAC address classification functions (optimized for performance)

// IsBroadcast checks if a MAC address is a broadcast address.
// Uses optimized byte comparison.
func IsBroadcast(addr net.HardwareAddr) bool {
	if len(addr) < 6 {
		return false
	}

	// Using direct comparison is faster than checking byte by byte
	return addr[0] == 0xff && addr[1] == 0xff &&
		addr[2] == 0xff && addr[3] == 0xff &&
		addr[4] == 0xff && addr[5] == 0xff
}

// IsMulticast checks if a MAC address is a multicast address.
// Optimized to use bit operations.
func IsMulticast(addr net.HardwareAddr) bool {
	return len(addr) > 0 && (addr[0]&0x01) == 0x01
}

// IsIPv4Multicast checks if a MAC address is an IPv4 multicast address.
func IsIPv4Multicast(addr net.HardwareAddr) bool {
	if len(addr) < 3 {
		return false
	}
	return addr[0] == 0x01 && addr[1] == 0x00 && addr[2] == 0x5e
}

// IsIPv6Multicast checks if a MAC address is an IPv6 multicast address.
func IsIPv6Multicast(addr net.HardwareAddr) bool {
	if len(addr) < 2 {
		return false
	}
	return addr[0] == 0x33 && addr[1] == 0x33
}

// Frame construction functions

// NewFrame creates a new MAC frame with the specified addresses and ethertype.
func NewFrame(dst, src net.HardwareAddr, ethertype Ethertype, payload []byte) []byte {
	frame := make([]byte, 14+len(payload))
	copy(frame[0:6], dst)
	copy(frame[6:12], src)
	frame[12] = ethertype.Hi
	frame[13] = ethertype.Lo
	copy(frame[14:], payload)
	return frame
}

// NewFrameInto writes a MAC frame into a pre-allocated buffer for better performance.
// It returns the number of bytes written or an error if the buffer is too small.
func NewFrameInto(buffer []byte, dst, src net.HardwareAddr, ethertype Ethertype, payload []byte) (int, error) {
	requiredSize := 14 + len(payload)
	if len(buffer) < requiredSize {
		return 0, errors.New("buffer too small")
	}

	copy(buffer[0:6], dst)
	copy(buffer[6:12], src)
	buffer[12] = ethertype.Hi
	buffer[13] = ethertype.Lo
	copy(buffer[14:], payload)

	return requiredSize, nil
}

// ParseFrame is an all-in-one parser that extracts all common fields from a frame.
// This is more efficient than calling individual functions when multiple fields are needed.
func ParseFrame(macFrame []byte) (dst, src net.HardwareAddr, tagging Tagging, ethertype Ethertype, payload []byte, err error) {
	if len(macFrame) < MinFrameSize {
		return nil, nil, NotTagged, Ethertype{}, nil, ErrFrameTooShort
	}

	// Extract addresses
	dst = FastDestination(macFrame)
	src = FastSource(macFrame)

	// Determine tagging
	tagging = FastTaggingType(macFrame)
	minSize := MinFrameSize

	if tagging == Tagged {
		minSize = MinTaggedFrameSize
	} else if tagging == DoubleTagged {
		minSize = MinDoubleTaggedSize
	}

	if len(macFrame) < minSize {
		return dst, src, tagging, Ethertype{}, nil, ErrFrameTooShort
	}

	// Extract ethertype and payload
	ethertype = FastEthertype(macFrame, tagging)
	payload = FastPayload(macFrame, tagging)

	return dst, src, tagging, ethertype, payload, nil
}

// Compatibility aliases for backward compatibility
var (
	MACDestination = Destination
	MACSource      = Source
	MACTagging     = DetectTagging
	MACEthertype   = GetEthertype
	MACPayload     = Payload
)

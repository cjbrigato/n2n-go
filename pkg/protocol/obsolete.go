package protocol

/*
import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/pearson"
	"net"
	"strconv"
	"strings"
	"time"
)

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// IHeader is the common interface for all header implementations
type IHeader interface {
	GetVersion() uint8
	GetPacketType() PacketType
	GetSequence() uint16
	GetSourceID() string
	GetDestinationID() string
	GetCommunity() string
	GetDestinationMAC() net.HardwareAddr
	VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool
	MarshalBinary() ([]byte, error)
	MarshalBinaryTo(buf []byte) error
	UnmarshalBinary(data []byte) error
}

// Header represents the legacy protocol header.
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

// Implement IHeader interface for Header
func (h *Header) GetVersion() uint8         { return h.Version }
func (h *Header) GetPacketType() PacketType { return h.PacketType }
func (h *Header) GetSequence() uint16       { return h.Sequence }

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

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// CompactHeader is an optimized network protocol header with reduced overhead
type CompactHeader struct {
	// Basic packet information (4 bytes)
	Version    uint8      // Protocol version
	TTL        uint8      // Time-to-live
	PacketType PacketType // Packet type (register, data, heartbeat, etc.)
	Flags      PacketFlag // Flags for various options

	// Identification (6 bytes)
	Sequence    uint16 // Sequence number
	CommunityID uint32 // Hash of community name - 32-bit to reduce collision risk

	// Addressing (12 bytes)
	SourceID [6]byte // Source identifier (MAC or hashed ID)
	DestID   [6]byte // Destination identifier (MAC or hashed ID)

	// Verification (8 bytes)
	Timestamp uint32 // Unix timestamp (seconds)
	Checksum  uint32 // CRC32 checksum

	// Context (not serialized)
	_sourceIDString  string // Original source ID for reference
	_destIDString    string // Original dest ID for reference
	_communityString string // Original community name for reference
}

// Implement IHeader interface for CompactHeader
func (h *CompactHeader) GetVersion() uint8         { return h.Version }
func (h *CompactHeader) GetPacketType() PacketType { return h.PacketType }
func (h *CompactHeader) GetSequence() uint16       { return h.Sequence }

// NewCompactHeader creates a new compact header
func NewCompactHeader(version, ttl uint8, pType PacketType, seq uint16, community, src, dest string) *CompactHeader {
	h := &CompactHeader{
		Version:          version,
		TTL:              ttl,
		PacketType:       pType,
		Flags:            0,
		Sequence:         seq,
		CommunityID:      HashCommunity(community),
		Timestamp:        uint32(time.Now().Unix()),
		_sourceIDString:  src,
		_destIDString:    dest,
		_communityString: community,
	}

	// Fill source ID (either MAC or hashed string ID)
	if mac, err := net.ParseMAC(src); err == nil && len(mac) >= 6 {
		copy(h.SourceID[:], mac[:6])
	} else {
		copy(h.SourceID[:], HashString(src, 6))
	}

	// Fill destination ID (either MAC or hashed string ID)
	if mac, err := net.ParseMAC(dest); err == nil && len(mac) >= 6 {
		copy(h.DestID[:], mac[:6])
	} else if dest != "" {
		copy(h.DestID[:], HashString(dest, 6))
	}

	// Set extended addressing flag if necessary
	/*if len(src) > 6 || len(dest) > 6 || len(community) > 10 {
		h.Flags |= FlagExtendedAddressing
	}*/
/*
	if pType == TypeRegister {
		h.SetExtendedAddressing(true)
	}

	return h
}

// NewCompactHeaderWithDestMAC creates a header with a MAC address as the destination
func NewCompactHeaderWithDestMAC(version, ttl uint8, pType PacketType, seq uint16, community, src string, dest net.HardwareAddr) *CompactHeader {
	h := NewCompactHeader(version, ttl, pType, seq, community, src, "")
	if len(dest) >= 6 {
		copy(h.DestID[:], dest[:6])
		h._destIDString = dest.String()
	}
	return h
}

// MarshalBinary serializes the compact header
func (h *CompactHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, CompactHeaderSize)

	if err := h.MarshalBinaryTo(buf); err != nil {
		return nil, err
	}

	return buf, nil
}

// MarshalBinaryTo serializes the compact header into the provided buffer
func (h *CompactHeader) MarshalBinaryTo(buf []byte) error {
	if len(buf) < CompactHeaderSize {
		return errors.New("buffer too small for compact header")
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

// UnmarshalBinary deserializes the compact header
func (h *CompactHeader) UnmarshalBinary(data []byte) error {
	if len(data) < CompactHeaderSize {
		return errors.New("insufficient data for compact header")
	}

	// Basic info
	h.Version = data[0]
	h.TTL = data[1]
	h.PacketType = PacketType(data[2])
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
	checksumData := make([]byte, CompactHeaderSize)
	copy(checksumData, data[:CompactHeaderSize])
	binary.BigEndian.PutUint32(checksumData[26:30], 0)

	actualChecksum := crc32.ChecksumIEEE(checksumData)
	if expectedChecksum != actualChecksum {
		return errors.New("checksum verification failed")
	}

	return nil
}

// GetSourceID returns the source ID
func (h *CompactHeader) GetSourceID() string {
	if h._sourceIDString != "" {
		return h._sourceIDString
	}
	// Return as MAC address format by default
	return net.HardwareAddr(h.SourceID[:]).String()
}

// GetDestinationID returns the destination ID
func (h *CompactHeader) GetDestinationID() string {
	if h._destIDString != "" {
		return h._destIDString
	}
	// Return as MAC address format by default
	return net.HardwareAddr(h.DestID[:]).String()
}

// GetCommunity returns the community name
func (h *CompactHeader) GetCommunity() string {
	return h._communityString
}

// GetDestinationMAC returns the destination MAC address
func (h *CompactHeader) GetDestinationMAC() net.HardwareAddr {
	// Check if DestID is all zeros
	empty := true
	for _, b := range h.DestID {
		if b != 0 {
			empty = false
			break
		}
	}

	if empty {
		return nil
	}

	mac := make(net.HardwareAddr, 6)
	copy(mac, h.DestID[:])
	return mac
}

// VerifyTimestamp checks if the header's timestamp is within the allowed drift
func (h *CompactHeader) VerifyTimestamp(ref time.Time, allowedDrift time.Duration) bool {
	ts := time.Unix(int64(h.Timestamp), 0)
	diff := ts.Sub(ref)
	if diff < 0 {
		diff = -diff
	}
	return diff <= allowedDrift
}

// SetExtendedAddressing sets the flag indicating extended addressing info in payload
func (h *CompactHeader) SetExtendedAddressing(value bool) {
	if value {
		h.Flags |= FlagExtendedAddressing
	} else {
		h.Flags &= ^FlagExtendedAddressing
	}
}

// HasExtendedAddressing returns whether extended addressing is being used
func (h *CompactHeader) HasExtendedAddressing() bool {
	return (h.Flags & FlagExtendedAddressing) != 0
}

// SetSourceIDString sets the original source ID string
func (h *CompactHeader) SetSourceIDString(id string) {
	h._sourceIDString = id
}

// SetDestIDString sets the original destination ID string
func (h *CompactHeader) SetDestIDString(id string) {
	h._destIDString = id
}

// SetCommunityString sets the original community string
func (h *CompactHeader) SetCommunityString(community string) {
	h._communityString = community

	// Update the community hash whenever the string changes
	h.CommunityID = HashCommunity(community)
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// ConvertToCompactHeader converts a legacy Header to a CompactHeader
func ConvertToCompactHeader(old *Header) *CompactHeader {
	h := &CompactHeader{
		Version:          VersionCompact,
		TTL:              old.TTL,
		PacketType:       old.PacketType,
		Sequence:         old.Sequence,
		Timestamp:        uint32(old.Timestamp / 1000000000), // Convert nano to seconds
		_sourceIDString:  trimNullString(old.SourceID[:]),
		_destIDString:    trimNullString(old.DestinationID[:]),
		_communityString: trimNullString(old.Community[:]),
	}

	// Calculate community hash
	h.CommunityID = HashCommunity(h._communityString)

	// Copy MAC address if present or hash the ID
	destMAC := old.GetDestinationMAC()
	if destMAC != nil {
		copy(h.DestID[:], destMAC)
	} else if h._destIDString != "" {
		copy(h.DestID[:], HashString(h._destIDString, 6))
	}

	// Hash source ID
	copy(h.SourceID[:], HashString(h._sourceIDString, 6))

	// Set extended addressing flag if needed
	if len(h._sourceIDString) > 6 || len(h._destIDString) > 6 || len(h._communityString) > 10 {
		h.Flags |= FlagExtendedAddressing
	}

	return h
}

// ConvertToLegacyHeader converts a CompactHeader to legacy Header
func ConvertToLegacyHeader(compact *CompactHeader) *Header {
	h := &Header{
		Version:    VersionLegacy,
		TTL:        compact.TTL,
		PacketType: compact.PacketType,
		Sequence:   compact.Sequence,
		Timestamp:  int64(compact.Timestamp) * 1000000000, // Convert seconds to nanoseconds
	}

	// Copy source ID string
	srcID := compact.GetSourceID()
	copy(h.SourceID[:], []byte(srcID))

	// Copy destination ID string
	destID := compact.GetDestinationID()
	copy(h.DestinationID[:], []byte(destID))

	// Copy community string
	community := compact.GetCommunity()
	copy(h.Community[:], []byte(community))

	return h
}

// ParseHeader detects and parses either a legacy or compact header
func ParseHeader(data []byte) (IHeader, error) {
	if len(data) < 1 {
		return nil, ErrInsufficientData
	}

	// Check version to determine header type
	version := data[0]

	switch version {
	case VersionLegacy:
		var h Header
		err := h.UnmarshalBinary(data)
		if err != nil {
			return nil, err
		}
		return &h, nil

	case VersionCompact:
		var h CompactHeader
		err := h.UnmarshalBinary(data)
		if err != nil {
			return nil, err
		}
		return &h, nil

	default:
		return nil, ErrUnsupportedVersion
	}
}

// CreateRegistrationPayload generates the payload for registration
func CreateRegistrationPayload(h IHeader, macAddr string) string {
	switch header := h.(type) {
	case *CompactHeader:
		if header.HasExtendedAddressing() {
			return fmt.Sprintf("REGISTER %s %s %s %d",
				header.GetSourceID(),
				macAddr,
				header.GetCommunity(),
				header.CommunityID)
		} else {
			return fmt.Sprintf("REGISTER %s %s",
				header.GetSourceID(),
				macAddr)
		}
	case *Header:
		return fmt.Sprintf("REGISTER %s %s",
			header.GetSourceID(),
			macAddr)
	default:
		return fmt.Sprintf("REGISTER unknown %s", macAddr)
	}
}

// Format: REGISTER <edgeID> <MAC> <community> <communityHash>
// ParseExtendedAddressing parses extended addressing information from a payload
func ParseExtendedAddressing(payload string, h *CompactHeader) {
	parts := strings.Fields(payload)
	if len(parts) >= 4 && parts[0] == "REGISTER" {
		// Extract the full source ID
		h.SetSourceIDString(parts[1])

		// Extract the community name if present
		if len(parts) >= 5 {
			h.SetCommunityString(parts[3])

			// If hash is included, verify it matches
			//if len(parts) >= 6 {
			hashValue, err := strconv.ParseUint(parts[4], 10, 32)
			if err == nil && HashCommunity(parts[3]) != uint32(hashValue) {
				// This is important - log the mismatch but don't fail registration
				// The supernode will validate this during registration
				fmt.Printf("Warning: Community hash mismatch for %s: expected %d, got %d\n",
					parts[3], HashCommunity(parts[3]), hashValue)
			}
			//}
		}
	}
}
*/

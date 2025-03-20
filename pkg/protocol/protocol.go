package protocol

import (
	"errors"
	"time"
)

const (
	// NoChecksum can be set to true to disable checksums for testing or performance
	NoChecksum = false

	// DefaultTimestampDrift is the default allowed timestamp drift for packets
	DefaultTimestampDrift = 16 * time.Second
)

// Common errors
var (
	ErrInsufficientData       = errors.New("insufficient data for header")
	ErrChecksumVerification   = errors.New("checksum verification failed")
	ErrTimestampVerification  = errors.New("timestamp verification failed")
	ErrUnsupportedVersion     = errors.New("unsupported header version")
	ErrCommunityHashCollision = errors.New("community hash collision detected")
)

type PacketFlag uint8

// Flag constants for compact header
const (
	FlagExtendedAddressing PacketFlag = 0x01 // Indicates full IDs are in payload
	FlagFromSuperNode      PacketFlag = 0x02
	FlagReserved2          PacketFlag = 0x04
	FlagReserved3          PacketFlag = 0x08
	FlagUserDefined1       PacketFlag = 0x10
	FlagUserDefined2       PacketFlag = 0x20
	FlagUserDefined3       PacketFlag = 0x40
	FlagUserDefined4       PacketFlag = 0x80
)

// PacketType defines the type of packet.
type PacketType uint8

const (
	TypeRegister     PacketType = 1
	TypeUnregister   PacketType = 2
	TypeHeartbeat    PacketType = 3
	TypeData         PacketType = 4
	TypeAck          PacketType = 5
	TypePeerRequest  PacketType = 6
	TypePeerInfo     PacketType = 7
	TypePing         PacketType = 8
	TypeP2PStateInfo PacketType = 9
	TypeP2PFullState PacketType = 10
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
	case TypePeerRequest:
		return "PeerRequest"
	case TypePeerInfo:
		return "PeerInfo"
	case TypePing:
		return "Ping"
	case TypeP2PStateInfo:
		return "P2PStateInfo"
	default:
		return "Unknown"
	}
}

// Helper functions for hashing

// HashString creates a hash of the given string of specified length
func HashString(s string, length int) []byte {
	if s == "" {
		return make([]byte, length)
	}

	hash := make([]byte, length)

	// Simple FNV-like hash algorithm
	for i, c := range s {
		hash[i%length] ^= byte(c) ^ byte(i)
	}

	return hash
}

// HashCommunity creates a 32-bit hash of a community name using FNV-1a algorithm
func HashCommunity(community string) uint32 {
	if community == "" {
		return 0
	}

	// FNV-1a hash algorithm for better distribution
	const prime uint32 = 16777619
	var hash uint32 = 2166136261 // FNV offset basis

	for _, c := range community {
		hash ^= uint32(c)
		hash *= prime
	}

	return hash
}

// VerifyCommunityHash checks if a community string produces the expected hash
func VerifyCommunityHash(community string, expectedHash uint32) bool {
	return HashCommunity(community) == expectedHash
}

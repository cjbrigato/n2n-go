package spec

// PacketType defines the type of packet.
type PacketType uint8

const (
	TypeRegisterRequest      PacketType = 1
	TypeUnregisterRequest    PacketType = 2
	TypeHeartbeat            PacketType = 3
	TypeData                 PacketType = 4
	TypeAck                  PacketType = 5
	TypePeerListRequest      PacketType = 6
	TypePeerInfo             PacketType = 7
	TypePing                 PacketType = 8
	TypeP2PStateInfo         PacketType = 9
	TypeP2PFullState         PacketType = 10
	TypeLeasesInfos          PacketType = 11
	TypeRegisterResponse     PacketType = 252
	TypeRetryRegisterRequest PacketType = 253
)

// String returns a human-readable name for the packet type
func (pt PacketType) String() string {
	switch pt {
	case TypeRegisterRequest:
		return "RegisterRequest"
	case TypeUnregisterRequest:
		return "UnregisterRequest"
	case TypeHeartbeat:
		return "Heartbeat"
	case TypeData:
		return "Data"
	case TypeAck:
		return "Ack"
	case TypePeerListRequest:
		return "PeerListRequest"
	case TypePeerInfo:
		return "PeerInfo"
	case TypePing:
		return "Ping"
	case TypeP2PStateInfo:
		return "P2PStateInfo"
	case TypeLeasesInfos:
		return "LeasesInfos"
	case TypeRegisterResponse:
		return "RegisterResponse"
	case TypeRetryRegisterRequest:
		return "RetryRegisterRequest"
	default:
		return "Unknown"
	}
}

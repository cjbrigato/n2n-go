package n2n

import (
	"time"
)

// Basic constants
const (
	N2NCommunitySize       = 16
	N2NPrivatePublicKeySize = 32
	N2NMacSize             = 6
	N2NDescSize            = 16
	N2NSockbufSize         = 48
	N2NAuthMaxTokenSize    = 48
	N2NAuthChallengeSize   = 16
	N2NIfnamsiz            = 16
	ETHAddrLen             = 6
)

// Type definitions for basic data structures
type N2NCommunity [N2NCommunitySize]byte
type N2NPrivatePublicKey [N2NPrivatePublicKeySize]byte
type N2NMac [N2NMacSize]byte
type N2NCookie uint32
type N2NDesc [N2NDescSize]byte
type N2NSockStr [N2NSockbufSize]byte
type N2NVersion [20]byte // N2N_VERSION_STRING_SIZE

// Constants for predefined MAC addresses
var (
	BroadcastMAC     = N2NMac{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	MulticastMAC     = N2NMac{0x01, 0x00, 0x5E, 0x00, 0x00, 0x00}
	IPv6MulticastMAC = N2NMac{0x33, 0x33, 0x00, 0x00, 0x00, 0x00}
	NullMAC          = N2NMac{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
)

// EtherHdr represents an Ethernet frame header
type EtherHdr struct {
	DHost [ETHAddrLen]byte `json:"dhost"`
	SHost [ETHAddrLen]byte `json:"shost"`
	Type  uint16           `json:"type"`
}

// N2NIPHdr represents IPv4 header with endianness handling
type N2NIPHdr struct {
	VersionIHL uint8  `json:"version_ihl"` // 4 bits version, 4 bits IHL
	TOS        uint8  `json:"tos"`
	TotLen     uint16 `json:"tot_len"`
	ID         uint16 `json:"id"`
	FragOff    uint16 `json:"frag_off"`
	TTL        uint8  `json:"ttl"`
	Protocol   uint8  `json:"protocol"`
	Check      uint16 `json:"check"`
	SAddr      uint32 `json:"saddr"`
	DAddr      uint32 `json:"daddr"`
}

// Helper methods for IPv4 header bit fields
func (h *N2NIPHdr) Version() uint8 { return h.VersionIHL >> 4 }
func (h *N2NIPHdr) IHL() uint8     { return h.VersionIHL & 0x0F }
func (h *N2NIPHdr) SetVersion(v uint8) { h.VersionIHL = (h.VersionIHL & 0x0F) | (v << 4) }
func (h *N2NIPHdr) SetIHL(ihl uint8)   { h.VersionIHL = (h.VersionIHL & 0xF0) | (ihl & 0x0F) }

// N2NTCPHdr represents a TCP header
type N2NTCPHdr struct {
	Source  uint16 `json:"source"`
	Dest    uint16 `json:"dest"`
	Seq     uint32 `json:"seq"`
	AckSeq  uint32 `json:"ack_seq"`
	Flags   uint16 `json:"flags"` // Includes data offset and all flags
	Window  uint16 `json:"window"`
	Check   uint16 `json:"check"`
	UrgPtr  uint16 `json:"urg_ptr"`
}

// Helper methods for TCP header bit fields
func (h *N2NTCPHdr) DataOffset() uint8 { return uint8((h.Flags >> 12) & 0x0F) }
func (h *N2NTCPHdr) FIN() bool { return (h.Flags & 0x0001) != 0 }
func (h *N2NTCPHdr) SYN() bool { return (h.Flags & 0x0002) != 0 }
func (h *N2NTCPHdr) RST() bool { return (h.Flags & 0x0004) != 0 }
func (h *N2NTCPHdr) PSH() bool { return (h.Flags & 0x0008) != 0 }
func (h *N2NTCPHdr) ACK() bool { return (h.Flags & 0x0010) != 0 }
func (h *N2NTCPHdr) URG() bool { return (h.Flags & 0x0020) != 0 }
func (h *N2NTCPHdr) ECE() bool { return (h.Flags & 0x0040) != 0 }
func (h *N2NTCPHdr) CWR() bool { return (h.Flags & 0x0080) != 0 }

// N2NUDPHdr represents a UDP header
type N2NUDPHdr struct {
	Source uint16 `json:"source"`
	Dest   uint16 `json:"dest"`
	Len    uint16 `json:"len"`
	Check  uint16 `json:"check"`
}

// PortRange represents a range of ports
type PortRange struct {
	StartPort uint16 `json:"start_port"` // Inclusive
	EndPort   uint16 `json:"end_port"`   // Inclusive
}

// FilterRuleKey uniquely identifies a filter rule
type FilterRuleKey struct {
	SrcNetCIDR        uint32    `json:"src_net_cidr"`
	SrcNetBitLen      uint8     `json:"src_net_bit_len"`
	SrcPortRange      PortRange `json:"src_port_range"`
	DstNetCIDR        uint32    `json:"dst_net_cidr"`
	DstNetBitLen      uint8     `json:"dst_net_bit_len"`
	DstPortRange      PortRange `json:"dst_port_range"`
	BoolTCPConfigured uint8     `json:"bool_tcp_configured"`
	BoolUDPConfigured uint8     `json:"bool_udp_configured"`
	BoolICMPConfigured uint8    `json:"bool_icmp_configured"`
}

// FilterRule represents a network traffic filter rule
type FilterRule struct {
	Key            FilterRuleKey `json:"key"`
	BoolAcceptICMP uint8         `json:"bool_accept_icmp"`
	BoolAcceptUDP  uint8         `json:"bool_accept_udp"`
	BoolAcceptTCP  uint8         `json:"bool_accept_tcp"`
}

// IPSubnet represents an IP subnet
type IPSubnet struct {
	NetAddr    uint32 `json:"net_addr"`    // Host order IP address
	NetBitLen  uint8  `json:"net_bitlen"`  // Subnet prefix length
}

// N2NSock represents a socket address (IPv4 or IPv6)
type N2NSock struct {
	Family  uint8    `json:"family"`  // AF_INET, AF_INET6 or AF_INVALID
	Type    uint8    `json:"type"`    // SOCK_STREAM or SOCK_DGRAM
	Port    uint16   `json:"port"`    // Host order
	Addr    [16]byte `json:"addr"`    // IPv4 or IPv6 address
}

// GetIPv4 returns the IPv4 address from the Addr field
func (s *N2NSock) GetIPv4() [4]byte {
	var result [4]byte
	copy(result[:], s.Addr[:4])
	return result
}

// SetIPv4 sets the IPv4 address in the Addr field
func (s *N2NSock) SetIPv4(addr [4]byte) {
	copy(s.Addr[:4], addr[:])
}

// N2NAuth represents authentication data
type N2NAuth struct {
	Scheme    uint16                     `json:"scheme"`      // Auth scheme type
	TokenSize uint16                     `json:"token_size"`  // Size of auth token
	Token     [N2NAuthMaxTokenSize]byte  `json:"token"`       // Auth data
}

// N2NCommon represents common packet header data
type N2NCommon struct {
	TTL       uint8         `json:"ttl"`
	PC        uint8         `json:"pc"`        // Packet type
	Flags     uint16        `json:"flags"`
	Community N2NCommunity  `json:"community"`
}

// Packet types as constants
const (
	N2NPing            = 0
	N2NRegister        = 1
	N2NDeregister      = 2
	N2NPacket          = 3
	N2NRegisterAck     = 4
	N2NRegisterSuper   = 5
	N2NUnregisterSuper = 6
	N2NRegisterSuperAck = 7
	N2NRegisterSuperNak = 8
	N2NFederation      = 9
	N2NPeerInfo        = 10
	N2NQueryPeer       = 11
	N2NReRegisterSuper = 12
)

// Flag values
const (
	N2NFlagsOptions       = 0x0080
	N2NFlagsSocket        = 0x0040
	N2NFlagsFromSupernode = 0x0020
	N2NFlagsTypeMask      = 0x001F
	N2NFlagsBitsMask      = 0xFFE0
)

// N2NRegister represents a registration packet
type N2NRegister struct {
	Cookie   N2NCookie `json:"cookie"`   // Link REGISTER and REGISTER_ACK
	SrcMac   N2NMac    `json:"srcMac"`   // MAC of registering party
	DstMac   N2NMac    `json:"dstMac"`   // MAC of target edge
	Sock     N2NSock   `json:"sock"`     // Supernode's view of edge socket
	DevAddr  IPSubnet  `json:"dev_addr"` // IP address of the tuntap adapter
	DevDesc  N2NDesc   `json:"dev_desc"` // Hint description for the edge
}

// N2NRegisterAck is a registration acknowledgment
type N2NRegisterAck struct {
	Cookie  N2NCookie `json:"cookie"`  // Return cookie from REGISTER
	SrcMac  N2NMac    `json:"srcMac"`  // MAC of acknowledging party
	DstMac  N2NMac    `json:"dstMac"`  // Reflected MAC of registering edge
	Sock    N2NSock   `json:"sock"`    // Supernode's view of edge socket
}

// N2NPacket represents a data packet
type N2NPacket struct {
	SrcMac      N2NMac  `json:"srcMac"`
	DstMac      N2NMac  `json:"dstMac"`
	Sock        N2NSock `json:"sock"`
	Transform   uint8   `json:"transform"`
	Compression uint8   `json:"compression"`
}

// N2NRegisterSuper is a supernode registration packet
type N2NRegisterSuper struct {
	Cookie   N2NCookie `json:"cookie"`
	EdgeMac  N2NMac    `json:"edgeMac"`
	Sock     N2NSock   `json:"sock"`
	DevAddr  IPSubnet  `json:"dev_addr"`
	DevDesc  N2NDesc   `json:"dev_desc"`
	Auth     N2NAuth   `json:"auth"`
	KeyTime  uint32    `json:"key_time"`
}

// N2NRegisterSuperAck is a supernode registration acknowledgment
type N2NRegisterSuperAck struct {
	Cookie    N2NCookie `json:"cookie"`
	SrcMac    N2NMac    `json:"srcMac"`
	DevAddr   IPSubnet  `json:"dev_addr"`
	Lifetime  uint16    `json:"lifetime"`
	Sock      N2NSock   `json:"sock"`
	Auth      N2NAuth   `json:"auth"`
	NumSN     uint8     `json:"num_sn"`
	KeyTime   uint32    `json:"key_time"`
}

// SNStats represents supernode statistics
type SNStats struct {
	Errors       int       `json:"errors"`
	RegSuper     int       `json:"reg_super"`
	RegSuperNak  int       `json:"reg_super_nak"`
	Fwd          int       `json:"fwd"`
	Broadcast    int       `json:"broadcast"`
	LastFwd      time.Time `json:"last_fwd"`
	LastRegSuper time.Time `json:"last_reg_super"`
}

// TransformType represents encryption types
type TransformType uint8

const (
	TransformInvalid  TransformType = 0
	TransformNull     TransformType = 1
	TransformTwofish  TransformType = 2
	TransformAES      TransformType = 3
	TransformChacha20 TransformType = 4
	TransformSpeck    TransformType = 5
)

// PeerInfo holds information about a peer
type PeerInfo struct {
	MacAddr           N2NMac    `json:"mac_addr"`
	DevAddr           IPSubnet  `json:"dev_addr"`
	DevDesc           N2NDesc   `json:"dev_desc"`
	Sock              N2NSock   `json:"sock"`
	SocketFD          int       `json:"socket_fd"`
	PreferredSock     N2NSock   `json:"preferred_sock"`
	LastCookie        N2NCookie `json:"last_cookie"`
	Auth              N2NAuth   `json:"auth"`
	Timeout           int       `json:"timeout"`
	Purgeable         bool      `json:"purgeable"`
	LastSeen          time.Time `json:"last_seen"`
	LastP2P           time.Time `json:"last_p2p"`
	LastSentQuery     time.Time `json:"last_sent_query"`
	SelectionCriterion int      `json:"selection_criterion"`
	LastValidTimeStamp uint64   `json:"last_valid_time_stamp"`
	IPAddr            string    `json:"ip_addr"`
	Local             uint8     `json:"local"`
	Uptime            time.Time `json:"uptime"`
	Version           N2NVersion `json:"version"`
}

// N2NSN represents a supernode
type N2NSN struct {
	KeepRunning       *bool          `json:"keep_running"`
	StartTime         time.Time      `json:"start_time"`
	Version           N2NVersion     `json:"version"`
	Stats             SNStats        `json:"stats"`
	Daemon            int            `json:"daemon"`
	MacAddr           N2NMac         `json:"mac_addr"`
	BindAddress       uint32         `json:"bind_address"`
	LPort             uint16         `json:"lport"`
	MPort             uint16         `json:"mport"`
	Sock              int            `json:"sock"`
	TCPSock           int            `json:"tcp_sock"`
	MgmtSock          int            `json:"mgmt_sock"`
	MinAutoIPNet      IPSubnet       `json:"min_auto_ip_net"`
	MaxAutoIPNet      IPSubnet       `json:"max_auto_ip_net"`
	LockCommunities   int            `json:"lock_communities"`
	CommunityFile     string         `json:"community_file"`
	PrivateKey        N2NPrivatePublicKey `json:"private_key"`
	Auth              N2NAuth        `json:"auth"`
	DynamicKeyTime    uint32         `json:"dynamic_key_time"`
	MgmtPasswordHash  uint64         `json:"mgmt_password_hash"`
}

// SNCommunity represents a community in the supernode
type SNCommunity struct {
	Community        [N2NCommunitySize]byte `json:"community"`
	IsFederation     uint8                  `json:"is_federation"`
	Purgeable        bool                   `json:"purgeable"`
	HeaderEncryption uint8                  `json:"header_encryption"`
	DynamicKey       [N2NAuthChallengeSize]byte `json:"dynamic_key"`
	NumberEncPackets int64                  `json:"number_enc_packets"`
	AutoIPNet        IPSubnet               `json:"auto_ip_net"`
}

// N2NEdgeStats holds statistics for an edge
type N2NEdgeStats struct {
	TxP2P          uint32 `json:"tx_p2p"`
	RxP2P          uint32 `json:"rx_p2p"`
	TxSup          uint32 `json:"tx_sup"`
	RxSup          uint32 `json:"rx_sup"`
	TxSupBroadcast uint32 `json:"tx_sup_broadcast"`
	RxSupBroadcast uint32 `json:"rx_sup_broadcast"`
}
package tuntap

import "fmt"

// Ethertype represents the Ethernet frame type field
type Ethertype struct {
	Hi, Lo byte
}

// Value returns the Ethertype as a uint16
func (e Ethertype) Value() uint16 {
	return uint16(e.Hi)<<8 | uint16(e.Lo)
}

// String returns a string representation of the Ethertype
func (e Ethertype) String() string {
	return fmt.Sprintf("0x%04x", e.Value())
}

// Common Ethertypes
var (
	EthertypeIPv4 = Ethertype{0x08, 0x00}
	EthertypeIPv6 = Ethertype{0x86, 0xdd}
	EthertypeARP  = Ethertype{0x08, 0x06}
)

var (
	IPv4                = Ethertype{0x08, 0x00}
	ARP                 = Ethertype{0x08, 0x06}
	WakeOnLAN           = Ethertype{0x08, 0x42}
	TRILL               = Ethertype{0x22, 0xF3}
	DECnetPhase4        = Ethertype{0x60, 0x03}
	RARP                = Ethertype{0x80, 0x35}
	AppleTalk           = Ethertype{0x80, 0x9B}
	AARP                = Ethertype{0x80, 0xF3}
	IPX1                = Ethertype{0x81, 0x37}
	IPX2                = Ethertype{0x81, 0x38}
	QNXQnet             = Ethertype{0x82, 0x04}
	IPv6                = Ethertype{0x86, 0xDD}
	EthernetFlowControl = Ethertype{0x88, 0x08}
	IEEE802_3           = Ethertype{0x88, 0x09}
	CobraNet            = Ethertype{0x88, 0x19}
	MPLSUnicast         = Ethertype{0x88, 0x47}
	MPLSMulticast       = Ethertype{0x88, 0x48}
	PPPoEDiscovery      = Ethertype{0x88, 0x63}
	PPPoESession        = Ethertype{0x88, 0x64}
	JumboFrames         = Ethertype{0x88, 0x70}
	HomePlug1_0MME      = Ethertype{0x88, 0x7B}
	IEEE802_1X          = Ethertype{0x88, 0x8E}
	PROFINET            = Ethertype{0x88, 0x92}
	HyperSCSI           = Ethertype{0x88, 0x9A}
	AoE                 = Ethertype{0x88, 0xA2}
	EtherCAT            = Ethertype{0x88, 0xA4}
	EthernetPowerlink   = Ethertype{0x88, 0xAB}
	LLDP                = Ethertype{0x88, 0xCC}
	SERCOS3             = Ethertype{0x88, 0xCD}
	HomePlugAVMME       = Ethertype{0x88, 0xE1}
	MRP                 = Ethertype{0x88, 0xE3}
	IEEE802_1AE         = Ethertype{0x88, 0xE5}
	IEEE1588            = Ethertype{0x88, 0xF7}
	IEEE802_1ag         = Ethertype{0x89, 0x02}
	FCoE                = Ethertype{0x89, 0x06}
	FCoEInit            = Ethertype{0x89, 0x14}
	RoCE                = Ethertype{0x89, 0x15}
	CTP                 = Ethertype{0x90, 0x00}
	VeritasLLT          = Ethertype{0xCA, 0xFE}
)

type IPProtocol byte

const (
	HOPOPT            = 0x00
	ICMP              = 0x01
	IGMP              = 0x02
	GGP               = 0x03
	IPv4Encapsulation = 0x04
	ST                = 0x05
	TCP               = 0x06
	CBT               = 0x07
	EGP               = 0x08
	IGP               = 0x09
	BBN_RCC_MON       = 0x0A
	NVP_II            = 0x0B
	PUP               = 0x0C
	ARGUS             = 0x0D
	EMCON             = 0x0E
	XNET              = 0x0F
	CHAOS             = 0x10
	UDP               = 0x11
	MUX               = 0x12
	DCN_MEAS          = 0x13
	HMP               = 0x14
	PRM               = 0x15
	XNS_IDP           = 0x16
	TRUNK_1           = 0x17
	TRUNK_2           = 0x18
	LEAF_1            = 0x19
	LEAF_2            = 0x1A
	RDP               = 0x1B
	IRTP              = 0x1C
	ISO_TP4           = 0x1D
	NETBLT            = 0x1E
	MFE_NSP           = 0x1F
	MERIT_INP         = 0x20
	DCCP              = 0x21
	ThirdPC           = 0x22
	IDPR              = 0x23
	XTP               = 0x24
	DDP               = 0x25
	IDPR_CMTP         = 0x26
	TPxx              = 0x27
	IL                = 0x28
	IPv6Encapsulation = 0x29
	SDRP              = 0x2A
	IPv6_Route        = 0x2B
	IPv6_Frag         = 0x2C
	IDRP              = 0x2D
	RSVP              = 0x2E
	GRE               = 0x2F
	MHRP              = 0x30
	BNA               = 0x31
	ESP               = 0x32
	AH                = 0x33
	I_NLSP            = 0x34
	SWIPE             = 0x35
	NARP              = 0x36
	MOBILE            = 0x37
	TLSP              = 0x38
	SKIP              = 0x39
	IPv6_ICMP         = 0x3A
	IPv6_NoNxt        = 0x3B
	IPv6_Opts         = 0x3C
	CFTP              = 0x3E
	SAT_EXPAK         = 0x40
	KRYPTOLAN         = 0x41
	RVD               = 0x42
	IPPC              = 0x43
	SAT_MON           = 0x45
	VISA              = 0x46
	IPCV              = 0x47
	CPNX              = 0x48
	CPHB              = 0x49
	WSN               = 0x4A
	PVP               = 0x4B
	BR_SAT_MON        = 0x4C
	SUN_ND            = 0x4D
	WB_MON            = 0x4E
	WB_EXPAK          = 0x4F
	ISO_IP            = 0x50
	VMTP              = 0x51
	SECURE_VMTP       = 0x52
	VINES             = 0x53
	TTP               = 0x54
	IPTM              = 0x54
	NSFNET_IGP        = 0x55
	DGP               = 0x56
	TCF               = 0x57
	EIGRP             = 0x58
	OSPF              = 0x59
	Sprite_RPC        = 0x5A
	LARP              = 0x5B
	MTP               = 0x5C
	AX_25             = 0x5D
	IPIP              = 0x5E
	MICP              = 0x5F
	SCC_SP            = 0x60
	ETHERIP           = 0x61
	ENCAP             = 0x62
	GMTP              = 0x64
	IFMP              = 0x65
	PNNI              = 0x66
	PIM               = 0x67
	ARIS              = 0x68
	SCPS              = 0x69
	QNX               = 0x6A
	A_N               = 0x6B
	IPComp            = 0x6C
	SNP               = 0x6D
	Compaq_Peer       = 0x6E
	IPX_in_IP         = 0x6F
	VRRP              = 0x70
	PGM               = 0x71
	L2TP              = 0x73
	DDX               = 0x74
	IATP              = 0x75
	STP               = 0x76
	SRP               = 0x77
	UTI               = 0x78
	SMP               = 0x79
	SM                = 0x7A
	PTP               = 0x7B
	FIRE              = 0x7D
	CRTP              = 0x7E
	CRUDP             = 0x7F
	SSCOPMCE          = 0x80
	IPLT              = 0x81
	SPS               = 0x82
	PIPE              = 0x83
	SCTP              = 0x84
	FC                = 0x85
	manet             = 0x8A
	HIP               = 0x8B
	Shim6             = 0x8C
)

package p2p

import (
	"n2n-go/pkg/protocol/spec"
	"net"
	"net/netip"
	"time"
)

type PeerInfoEventType uint8

const (
	TypeList       PeerInfoEventType = 1
	TypeRegister   PeerInfoEventType = 2
	TypeUnregister PeerInfoEventType = 3
)

type PeerCachedInfo struct {
	Desc       string
	MACAddr    string
	VirtualIP  netip.Addr
	Community  string
	LastUpdate time.Time
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	//PrivSocket *net.UDPAddr     `json:"privSocket"`
	Community string `json:"community"`
	Desc      string `json:"desc"`
}

type PeerInfoList struct {
	HasOrigin bool
	Origin    PeerInfo
	PeerInfos []PeerInfo
	EventType PeerInfoEventType
}

func (pil *PeerInfoList) PacketType() spec.PacketType {
	return spec.TypePeerInfo
}

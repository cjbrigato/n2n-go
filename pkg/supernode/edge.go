package supernode

import (
	"n2n-go/pkg/p2p"
	"net"
	"net/netip"
	"time"
)

// Edge represents a registered edge.
type Edge struct {
	Desc          string // from header.SourceID
	PublicIP      net.IP // Public IP address
	PublicPort    int    // UDP port
	LocalIP       net.IP
	LocalPort     int
	Community     string     // Community name
	VirtualIP     netip.Addr // Virtual IP assigned within the community
	VNetMaskLen   int        // CIDR mask length for the virtual network
	LastHeartbeat time.Time  // Time of last heartbeat
	LastSequence  uint16     // Last sequence number received
	MACAddr       string     // MAC address provided during registration
	MachineID     []byte
}

func (e *Edge) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: e.PublicIP, Port: e.PublicPort}
}

func (e *Edge) PeerInfo() p2p.PeerInfo {
	mac, _ := net.ParseMAC(e.MACAddr)
	return p2p.PeerInfo{
		VirtualIP: e.VirtualIP,
		MACAddr:   mac,
		PubSocket: &net.UDPAddr{
			IP:   e.PublicIP,
			Port: e.PublicPort,
		},
		Community: e.Community,
		Desc:      e.Desc,
	}
}

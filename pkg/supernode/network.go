package supernode

import (
	"crypto/sha256"
	"encoding/binary"
	"net"
	"net/netip"
)

type NetworkAllocator struct {
	baseNetwork net.IP
	mask        net.IPMask
}

// NewNetworkAllocator creates a new NetworkAllocator given a base network and mask.
// For example, NewNetworkAllocator(net.ParseIP("10.128.0.0"), net.CIDRMask(24, 32)).
func NewNetworkAllocator(baseNetwork net.IP, mask net.IPMask) *NetworkAllocator {
	return &NetworkAllocator{
		baseNetwork: baseNetwork.To4(),
		mask:        mask,
	}
}

// Programatically propose a network for a given community
func (m *NetworkAllocator) ProposeVirtualNetwork(community string) (netip.Prefix, error) {
	// Compute a SHA-256 hash of the community.
	h := sha256.Sum256([]byte(community))
	// Use the lower 24 bits of the hash as an offset.
	offset := binary.BigEndian.Uint32(h[:4]) & 0x00FFFFFF

	// Get the base network as a uint32.
	base := binary.BigEndian.Uint32(m.baseNetwork)
	// Combine the base network with the offset.
	vipUint := base | offset

	// Convert the uint32 back to an IP.
	vip := make(net.IP, 4)
	binary.BigEndian.PutUint32(vip, vipUint)
	o, _ := m.mask.Size()
	addr, _ := netip.AddrFromSlice(vip.To4())
	return addr.Prefix(o)
}

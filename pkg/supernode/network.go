package supernode

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sync"
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

type AddrPool struct {
	pool      []netip.Addr
	available []int
	uses      map[string]int
	maskLen   int
	mu        sync.Mutex
}

func NewAddrPool(subnet netip.Prefix) *AddrPool {
	pool := make([]netip.Addr, 0)
	available := make([]int, 0)
	p := subnet.Masked()
	addr := p.Addr().Next() //dont list network ip
	last := addr
	once := false
	for {
		if !p.Contains(addr) {
			break
		}
		if once {
			pool = append(pool, last)                     // use last so we've already break if at broadcast
			available = append(available, len(available)) // starts at 0 , next is index for last appended to the pool
		}
		once = true
		last = addr
		addr = addr.Next()
	}
	return &AddrPool{
		pool:      pool,
		available: available,
		uses:      make(map[string]int),
		maskLen:   p.Bits(),
	}
}

func (p *AddrPool) addrFromId(poolid int) (netip.Addr, int, error) {
	if poolid < 0 || poolid > len(p.pool)-1 {
		return netip.Addr{}, -1, fmt.Errorf("invalid pool ressource id")
	}
	return p.pool[poolid], p.maskLen, nil
}

func (p *AddrPool) Request(edgeid string) (netip.Addr, int, error) {
	v, exists := p.uses[edgeid]
	if exists {
		return p.addrFromId(v)
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.available) < 1 {
		return netip.Addr{}, -1, fmt.Errorf("no available VIP addresses")
	}

	poolid := p.available[0]
	p.available = p.available[1:]
	p.uses[edgeid] = poolid

	return p.addrFromId(poolid)
}

func (p *AddrPool) Release(edgeid string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	poolid, exists := p.uses[edgeid]
	if !exists {
		return fmt.Errorf("unknown edgeid / no lease known for this edgeid")
	}

	p.available = append(p.available, poolid)
	delete(p.uses, edgeid)

	return nil
}

package supernode

import (
	"fmt"
	"net/netip"
	"sync"
)

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

// VIPPool manages VIP allocation for a community.
type VIPPool struct {
	*AddrPool
}

func NewVIPPool(prefix netip.Prefix) *VIPPool {
	return &VIPPool{
		AddrPool: NewAddrPool(prefix),
	}
}

func (pool *VIPPool) Allocate(edgeID string) (netip.Addr, int, error) {
	return pool.Request(edgeID)
}

func (pool *VIPPool) Free(edgeID string) {
	pool.Release(edgeID)
}

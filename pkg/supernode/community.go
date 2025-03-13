package supernode

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Community struct {
	name     string
	subnet   netip.Prefix
	addrPool *AddrPool

	edgeMu    sync.Mutex
	edges     map[string]*Edge
	macMu     sync.Mutex
	macToEdge map[string]string
}

func NewCommunity(name string, subnet netip.Prefix) *Community {
	return &Community{
		name:      name,
		subnet:    subnet,
		addrPool:  NewAddrPool(subnet),
		edges:     make(map[string]*Edge),
		macToEdge: make(map[string]string),
	}
}

func (c *Community) Unregister(srcID string) bool {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	if edge, exists := c.edges[srcID]; exists {
		err := c.addrPool.Release(srcID)
		if err != nil {
			if debug {
				log.Printf("Supernode: VIP Release failed for community %s: %v", edge.Community, err)
			}
		}
		c.macMu.Lock()
		delete(c.macToEdge, edge.MACAddr)
		c.macMu.Unlock()
		delete(c.edges, srcID)
		log.Printf("Supernode: -edge.unregister: id=%s, community=%s, freed VIP=%s, MAC=%s, ", srcID, edge.Community, edge.VirtualIP.String(), edge.MACAddr)
		return true
	}
	return false
}

func (c *Community) EdgeUpdate(srcID string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) (*Edge, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	var macAddr net.HardwareAddr
	if isReg {
		parts := strings.Fields(payload)
		if len(parts) >= 3 {
			// Parse the MAC address.
			mac, err := net.ParseMAC(parts[2])
			if err != nil {
				log.Printf("Supernode: Failed to parse MAC address %s: %v", parts[2], err)
				return nil, err
			} else {
				macAddr = mac
			}
		} else {
			log.Printf("Supernode: Registration payload format invalid: %s", payload)
			return nil, fmt.Errorf("%s.communities: Registration payload format invalid: %s", c.name, payload)
		}
	}
	edge, exists := c.edges[srcID]
	if !exists {
		vip, masklen, err := c.addrPool.Request(srcID)
		if err != nil {
			if debug {
				log.Printf("Supernode: VIP allocation failed for edge %s: %v", srcID, err)
			}
			return nil, err
		}
		edge = &Edge{
			ID:            srcID,
			PublicIP:      addr.IP,
			Port:          addr.Port,
			Community:     c.name,
			VirtualIP:     vip,
			VNetMaskLen:   masklen,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
			MACAddr:       macAddr.String(), // We store MAC as empty string in Edge, but update mapping below.
		}
		c.edges[srcID] = edge
		c.macMu.Lock()
		c.macToEdge[edge.MACAddr] = srcID // store edge ID keyed by MAC string
		c.macMu.Unlock()
		log.Printf("Supernode: +edge.register: id=%s, community=%s, assigned VIP=%s, MAC=%s", srcID, c.name, vip, edge.MACAddr)
	} else {
		if c.name != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", srcID, c.name, edge.Community)
			return nil, fmt.Errorf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", srcID, c.name, edge.Community)
		}
		edge.PublicIP = addr.IP
		edge.Port = addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		if isReg {
			edge.MACAddr = macAddr.String()
			c.macMu.Lock()
			c.macToEdge[edge.MACAddr] = srcID
			c.macMu.Unlock()
		}
		if debug {
			log.Printf("Supernode: ~edge.update: id=%s, community=%s", srcID, c.name)
		}
	}
	return edge, nil
}

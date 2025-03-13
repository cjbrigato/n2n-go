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

// Community represents a logical grouping of edges sharing the same virtual network
type Community struct {
	name     string       // Name of the community
	subnet   netip.Prefix // Network prefix for this community
	addrPool *AddrPool    // Pool of available IP addresses
	config   *Config      // Reference to the global configuration

	edgeMu    sync.RWMutex      // Protects edges map
	edges     map[string]*Edge  // Map of edges by ID
	macMu     sync.RWMutex      // Protects macToEdge map
	macToEdge map[string]string // Maps MAC addresses to edge IDs
}

// NewCommunity creates a new community with the specified name and subnet
func NewCommunity(name string, subnet netip.Prefix) *Community {
	return &Community{
		name:      name,
		subnet:    subnet,
		addrPool:  NewAddrPool(subnet),
		edges:     make(map[string]*Edge),
		macToEdge: make(map[string]string),
		config:    DefaultConfig(), // Use default config if none specified
	}
}

// NewCommunityWithConfig creates a new community with the specified configuration
func NewCommunityWithConfig(name string, subnet netip.Prefix, config *Config) *Community {
	c := NewCommunity(name, subnet)
	c.config = config
	return c
}

// debugLog logs a message if debug mode is enabled
func (c *Community) debugLog(format string, args ...interface{}) {
	if c.config.Debug {
		log.Printf("Community[%s] DEBUG: "+format, append([]interface{}{c.name}, args...)...)
	}
}

// Unregister removes an edge from the community and releases its IP address
func (c *Community) Unregister(srcID string) bool {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()

	edge, exists := c.edges[srcID]
	if !exists {
		return false
	}

	// Release the IP address
	err := c.addrPool.Release(srcID)
	if err != nil {
		c.debugLog("VIP Release failed: %v", err)
	}

	// Remove MAC address mapping
	c.macMu.Lock()
	delete(c.macToEdge, edge.MACAddr)
	c.macMu.Unlock()

	// Remove the edge
	delete(c.edges, srcID)

	log.Printf("Community[%s]: Unregistered edge: id=%s, freed VIP=%s, MAC=%s",
		c.name, srcID, edge.VirtualIP.String(), edge.MACAddr)

	return true
}

// EdgeUpdate registers a new edge or updates an existing one
func (c *Community) EdgeUpdate(srcID string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) (*Edge, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()

	var macAddr net.HardwareAddr

	// Extract MAC address from registration payload
	if isReg {
		parts := strings.Fields(payload)
		if len(parts) >= 3 {
			// Parse the MAC address
			mac, err := net.ParseMAC(parts[2])
			if err != nil {
				log.Printf("Community[%s]: Failed to parse MAC address %s: %v", c.name, parts[2], err)
				return nil, fmt.Errorf("failed to parse MAC address %s: %w", parts[2], err)
			}
			macAddr = mac
		} else {
			log.Printf("Community[%s]: Registration payload format invalid: %s", c.name, payload)
			return nil, fmt.Errorf("registration payload format invalid: %s", payload)
		}
	}

	// Check if edge already exists
	edge, exists := c.edges[srcID]

	if !exists {
		// New edge, allocate an IP address
		vip, masklen, err := c.addrPool.Request(srcID)
		if err != nil {
			c.debugLog("VIP allocation failed for edge %s: %v", srcID, err)
			return nil, fmt.Errorf("IP allocation failed: %w", err)
		}

		// Create new edge
		edge = &Edge{
			ID:            srcID,
			PublicIP:      addr.IP,
			Port:          addr.Port,
			Community:     c.name,
			VirtualIP:     vip,
			VNetMaskLen:   masklen,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
			MACAddr:       macAddr.String(),
		}

		c.edges[srcID] = edge

		// Update MAC to edge mapping
		if macAddr != nil {
			c.macMu.Lock()
			c.macToEdge[edge.MACAddr] = srcID
			c.macMu.Unlock()
		}

		log.Printf("Community[%s]: Registered new edge: id=%s, assigned VIP=%s, MAC=%s",
			c.name, srcID, vip, edge.MACAddr)
	} else {
		// Existing edge, update information
		if c.name != edge.Community {
			log.Printf("Community[%s]: Community mismatch for edge %s: received %q vs registered %q",
				c.name, srcID, c.name, edge.Community)
			return nil, fmt.Errorf("community mismatch for edge %s", srcID)
		}

		// Update edge information
		edge.PublicIP = addr.IP
		edge.Port = addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq

		// If this is a registration packet, update MAC address
		if isReg && macAddr != nil {
			// Remove old MAC mapping if different
			if edge.MACAddr != macAddr.String() {
				c.macMu.Lock()
				delete(c.macToEdge, edge.MACAddr)
				c.macMu.Unlock()
			}

			edge.MACAddr = macAddr.String()

			c.macMu.Lock()
			c.macToEdge[edge.MACAddr] = srcID
			c.macMu.Unlock()
		}

		c.debugLog("Updated edge: id=%s", srcID)
	}

	return edge, nil
}

// GetEdgeByMAC retrieves an edge by its MAC address
func (c *Community) GetEdgeByMAC(mac string) (*Edge, bool) {
	c.macMu.RLock()
	edgeID, exists := c.macToEdge[mac]
	c.macMu.RUnlock()

	if !exists {
		return nil, false
	}

	c.edgeMu.RLock()
	edge, exists := c.edges[edgeID]
	c.edgeMu.RUnlock()

	if !exists {
		// Clean up stale MAC mapping
		c.macMu.Lock()
		delete(c.macToEdge, mac)
		c.macMu.Unlock()
		return nil, false
	}

	// Return a copy to prevent races
	edgeCopy := *edge
	return &edgeCopy, true
}

// GetAllEdges returns a copy of all edges in the community
func (c *Community) GetAllEdges() []*Edge {
	c.edgeMu.RLock()
	defer c.edgeMu.RUnlock()

	edges := make([]*Edge, 0, len(c.edges))
	for _, edge := range c.edges {
		edgeCopy := *edge
		edges = append(edges, &edgeCopy)
	}

	return edges
}

// GetEdgeByID retrieves an edge by its ID
func (c *Community) GetEdgeByID(id string) (*Edge, bool) {
	c.edgeMu.RLock()
	edge, exists := c.edges[id]
	c.edgeMu.RUnlock()

	if !exists {
		return nil, false
	}

	// Return a copy to prevent races
	edgeCopy := *edge
	return &edgeCopy, true
}

// Size returns the number of edges in the community
func (c *Community) Size() int {
	c.edgeMu.RLock()
	defer c.edgeMu.RUnlock()
	return len(c.edges)
}

// Name returns the name of the community
func (c *Community) Name() string {
	return c.name
}

// Subnet returns the subnet assigned to this community
func (c *Community) Subnet() netip.Prefix {
	return c.subnet
}

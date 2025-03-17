package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/peer"
	"n2n-go/pkg/protocol"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Community represents a logical grouping of edges sharing the same virtual network
type Community struct {
	name     string       // Name of the community
	subnet   netip.Prefix // Network prefix for this community
	addrPool *AddrPool    // Pool of available IP addresses
	config   *Config      // Reference to the global configuration

	edgeMu sync.RWMutex     // Protects edges map
	edges  map[string]*Edge // Map of edges by MACAddrString     // Map of edges by ID
	//macMu     sync.RWMutex      // Protects macToEdge map
	//macToEdge map[string]string // Maps MAC addresses to edge IDs
}

// NewCommunity creates a new community with the specified name and subnet
func NewCommunity(name string, subnet netip.Prefix) *Community {
	return &Community{
		name:     name,
		subnet:   subnet,
		addrPool: NewAddrPool(subnet),
		edges:    make(map[string]*Edge),
		config:   DefaultConfig(), // Use default config if none specified
	}
}

// NewCommunityWithConfig creates a new community with the specified configuration
func NewCommunityWithConfig(name string, subnet netip.Prefix, config *Config) *Community {
	c := NewCommunity(name, subnet)
	c.config = config
	return c
}

func (c *Community) GetPeerInfoList(reqMACAddr string, full bool) peer.PeerInfoList {
	edges := c.GetAllEdges()
	var pis []peer.PeerInfo
	for _, e := range edges {
		if e.MACAddr == reqMACAddr && !full {
			continue
		}
		pis = append(pis, e.PeerInfo())
	}
	return peer.PeerInfoList{PeerInfos: pis, EventType: peer.TypeList}
}

func (c *Community) GetEdgeUDPAddr(MACAddr string) (*net.UDPAddr, error) {
	c.edgeMu.RLock()
	edge, exists := c.edges[MACAddr]
	c.edgeMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("Community[%s]: unknown edgeMacAddr: %v", c.Name(), MACAddr)
	}
	return edge.UDPAddr(), nil
}

// debugLog logs a message if debug mode is enabled
func (c *Community) debugLog(format string, args ...interface{}) {
	if c.config.Debug {
		log.Printf("Community[%s] DEBUG: "+format, append([]interface{}{c.name}, args...)...)
	}
}

// Unregister removes an edge from the community and releases its IP address
func (c *Community) Unregister(edgeMACAddr string) bool {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()

	edge, exists := c.edges[edgeMACAddr]
	if !exists {
		log.Printf("Community[%s]: cannot unregister unknown edgeMacAddr: %v", c.Name(), edgeMACAddr)
		return false
	}

	// Release the IP address
	err := c.addrPool.Release(edgeMACAddr)
	if err != nil {
		c.debugLog("VIP Release failed: %v", err)
	}

	delete(c.edges, edgeMACAddr)

	log.Printf("Community[%s]: Unregistered edge \"%s\": id=%s, freed VIP=%s",
		c.name, edge.Desc, edge.MACAddr, edge.VirtualIP.String())
	return true
}

func (c *Community) RefreshEdge(hbMsg *protocol.HeartbeatMessage) (bool, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	edge, exists := c.edges[hbMsg.EdgeMACAddr]
	if !exists {
		return false, fmt.Errorf("Community:%s unknown edge:%s cannot be refreshed", c.name, hbMsg.EdgeMACAddr)
	}
	oldPort := edge.Port
	oldPublicIP := edge.PublicIP
	edge.PublicIP = hbMsg.RawMsg.Addr.IP
	edge.Port = hbMsg.RawMsg.Addr.Port
	edge.LastHeartbeat = time.Now()
	edge.LastSequence = hbMsg.RawMsg.Header.Sequence
	c.debugLog("Refreshed edge:%s from HeartBeat", c.name, hbMsg.EdgeMACAddr)
	return (oldPort != hbMsg.RawMsg.Addr.Port) || (!oldPublicIP.Equal(hbMsg.RawMsg.Addr.IP)), nil
}

// EdgeUpdate registers a new edge or updates an existing one
func (c *Community) EdgeUpdate(regMsg *protocol.RegisterMessage) (*Edge, error) { //srcID string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) (*Edge, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()

	// Check if edge already exists
	edge, exists := c.edges[regMsg.EdgeMACAddr]

	if !exists {
		// New edge, allocate an IP address
		vip, masklen, err := c.addrPool.Request(regMsg.EdgeMACAddr)
		if err != nil {
			c.debugLog("VIP allocation failed for edge %s: %v", regMsg.EdgeMACAddr, err)
			return nil, fmt.Errorf("IP allocation failed: %w", err)
		}

		// Create new edge
		edge = &Edge{
			Desc:          regMsg.EdgeDesc,
			PublicIP:      regMsg.RawMsg.Addr.IP,
			Port:          regMsg.RawMsg.Addr.Port,
			Community:     c.name,
			VirtualIP:     vip,
			VNetMaskLen:   masklen,
			LastHeartbeat: time.Now(),
			LastSequence:  regMsg.RawMsg.Header.Sequence,
			MACAddr:       regMsg.EdgeMACAddr,
		}

		c.edges[regMsg.EdgeMACAddr] = edge

		log.Printf("Community[%s]: Registered new edge \"%s\" id=%s, assigned VIP=%s",
			c.name, edge.Desc, edge.MACAddr, vip)
	} else {
		// Existing edge, update information
		if c.name != edge.Community {
			log.Printf("Community[%s]: Community mismatch for edge %s/%s: received %q vs registered %q",
				c.name, edge.Desc, edge.MACAddr, c.name, edge.Community)
			return nil, fmt.Errorf("community mismatch for edge %s", edge.MACAddr)
		}

		// Update edge information
		edge.PublicIP = regMsg.RawMsg.Addr.IP
		edge.Port = regMsg.RawMsg.Addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = regMsg.RawMsg.Header.Sequence

		c.debugLog("Updated edge: id=%s", edge.MACAddr)
	}

	return edge, nil
}

// GetEdgeByMAC retrieves an edge by its MAC address
func (c *Community) GetEdge(macAddr string) (*Edge, bool) {
	c.edgeMu.RLock()
	edge, exists := c.edges[macAddr]
	c.edgeMu.RUnlock()

	if !exists {
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

// GetAllEdges returns a copy of all edges in the community
func (c *Community) GetStaleEdgeIDs(expiry time.Duration) []string {
	now := time.Now()
	c.edgeMu.RLock()
	defer c.edgeMu.RUnlock()
	ids := []string{}
	for _, edge := range c.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			ids = append(ids, edge.MACAddr)
		}
	}
	return ids
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

// Name returns the name of the community
func (c *Community) Hash() uint32 {
	return protocol.HashCommunity(c.name)
}

// Subnet returns the subnet assigned to this community
func (c *Community) Subnet() netip.Prefix {
	return c.subnet
}

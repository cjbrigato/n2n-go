package supernode

import (
	"bytes"
	"fmt"
	"n2n-go/pkg/log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/cjbrigato/ippool"
)

type EdgeAddressable interface {
	EdgeMACAddr() string
	CommunityHash() uint32
}

// Community represents a logical grouping of edges sharing the same virtual network
type Community struct {
	name   string       // Name of the community
	subnet netip.Prefix // Network prefix for this community
	//addrPool *AddrPool    // Pool of available IP addresses
	config *Config // Reference to the global configuration

	ips     *ippool.IPPool
	maskLen int

	p2pMu                 sync.RWMutex
	communityPeerP2PInfos map[string]p2p.PeerP2PInfos // known PeerP2PInfos keyed by edgeID (MACAddrString)

	edgeMu sync.RWMutex     // Protects edges map
	edges  map[string]*Edge // Map of edges by MACAddrString     // Map of edges by ID
	//macMu     sync.RWMutex      // Protects macToEdge map
	//macToEdge map[string]string // Maps MAC addresses to edge IDs

	sn *Supernode
}

// NewCommunity creates a new community with the specified name and subnet
func NewCommunity(name string, subnet netip.Prefix, sn *Supernode) (*Community, error) {
	d := subnet.Masked().Bits()
	cidr := fmt.Sprintf("%s/%d", subnet.Masked().Addr().String(), d)
	var err error
	ips, ok := ippool.GetRegisteredPool(cidr)
	if !ok {
		ips, err = ippool.NewIPPool(cidr, 24*time.Hour)
		if err != nil {
			return nil, err
		}
	}
	return &Community{
		name:   name,
		subnet: subnet,
		//addrPool:              NewAddrPool(subnet),
		ips:                   ips,
		maskLen:               d,
		edges:                 make(map[string]*Edge),
		communityPeerP2PInfos: make(map[string]p2p.PeerP2PInfos),
		config:                DefaultConfig(), // Use default config if none specified
		sn:                    sn,
	}, nil
}

// NewCommunityWithConfig creates a new community with the specified configuration
func NewCommunityWithConfig(name string, subnet netip.Prefix, config *Config, sn *Supernode) (*Community, error) {
	c, err := NewCommunity(name, subnet, sn)
	if err != nil {
		return nil, err
	}
	c.config = config
	return c, nil
}

func (c *Community) SetEdgeCachedInfo(macAddr string, desc string, isRegistered bool, vip netip.Addr) {
	c.sn.SetEdgeCachedInfo(macAddr, desc, c.Name(), isRegistered, vip)
}

func (c *Community) GetEdgeCachedInfo(macAddr string) (*EdgeCachedInfos, bool) {
	return c.sn.GetEdgeCachedInfo(macAddr)
}

func (c *Community) ResetP2PInfos() {
	c.p2pMu.Lock()
	defer c.p2pMu.Unlock()
	c.communityPeerP2PInfos = make(map[string]p2p.PeerP2PInfos)
	log.Printf("Community[%s]: reseted communityPeerP2PInfos", c.Name())
}

func (c *Community) SetP2PInfosFor(edgeMacADDR string, infos *p2p.PeerP2PInfos) error {
	c.edgeMu.RLock()
	_, exists := c.edges[edgeMacADDR]
	c.edgeMu.RUnlock()
	if !exists {
		return fmt.Errorf("Community:%s unknown edge:%s cannot set P2PInfosFor", c.name, edgeMacADDR)
	}
	c.p2pMu.Lock()
	c.communityPeerP2PInfos[edgeMacADDR] = *infos
	c.p2pMu.Unlock()
	return nil
}

/*
func (c *Community) CommunityP2PState() (*p2p.CommunityP2PVizDatas, error) {
	return p2p.NewCommunityP2PVizDatas(c.Name(), c.communityPeerP2PInfos,c.)
}
*/

func (c *Community) GetCommunityPeerP2PInfosDatas(edgeMacADDR string) (*p2p.P2PFullState, error) {
	c.edgeMu.RLock()
	_, exists := c.edges[edgeMacADDR]
	c.edgeMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("Community:%s unknown edge:%s cannot set P2PInfosFor", c.name, edgeMacADDR)
	}
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	unreachables, err := c.sn.GetOfflineCachedEdges(c)
	if err != nil {
		return nil, err
	}
	return &p2p.P2PFullState{
		CommunityName: c.Name(),
		IsRequest:     false,
		P2PCommunityDatas: p2p.P2PCommunityDatas{
			Reachables:   c.communityPeerP2PInfos,
			UnReachables: unreachables,
		},
	}, nil
}

func (c *Community) GetPeerInfoList(reqMACAddr string, full bool) *p2p.PeerInfoList {
	edges := c.GetAllEdges()
	var origin p2p.PeerInfo
	var pis []p2p.PeerInfo
	var hasOrigin bool
	for _, e := range edges {
		if e.MACAddr == reqMACAddr {
			if full {
				origin = e.PeerInfo()
				hasOrigin = true
			}
			continue
		}
		pis = append(pis, e.PeerInfo())
	}
	return &p2p.PeerInfoList{Origin: origin, HasOrigin: hasOrigin, PeerInfos: pis, EventType: p2p.TypeList}
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

	c.SetEdgeCachedInfo(edgeMACAddr, edge.Desc, false, edge.VirtualIP)
	delete(c.edges, edgeMACAddr)
	c.p2pMu.Lock()
	defer c.p2pMu.Unlock()
	delete(c.communityPeerP2PInfos, edgeMACAddr)

	log.Printf("Community[%s]: Unregistered edge \"%s\": id=%s,vip=%s",
		c.name, edge.Desc, edge.MACAddr, edge.VirtualIP.String())
	return true
}

func (c *Community) RefreshEdge(hbMsg *protocol.Message[*netstruct.HeartbeatPulse]) (bool, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()
	edge, exists := c.edges[hbMsg.EdgeMACAddr()]
	if !exists {
		return false, fmt.Errorf("Community:%s unknown edge:%s cannot be refreshed", c.name, hbMsg.EdgeMACAddr())
	}
	if !bytes.Equal(hbMsg.Msg.ClearMachineID, edge.MachineID) {
		return false, fmt.Errorf("Community:%s wrong decrypted machineID for edge :%s cannot be refreshed", c.name, hbMsg.EdgeMACAddr())
	}
	oldPort := edge.PublicPort
	oldPublicIP := edge.PublicIP
	edge.PublicIP = hbMsg.FromAddr.IP
	edge.PublicPort = hbMsg.FromAddr.Port
	edge.LastHeartbeat = time.Now()
	edge.LastSequence = hbMsg.Header.Sequence
	c.debugLog("Refreshed edge:%s from HeartBeat", c.name, hbMsg.EdgeMACAddr)
	return (oldPort != hbMsg.FromAddr.Port) || (!oldPublicIP.Equal(hbMsg.FromAddr.IP)), nil
}

// EdgeUpdate registers a new edge or updates an existing one
func (c *Community) EdgeUpdate(regMsg *protocol.Message[*netstruct.RegisterRequest]) (*Edge, error) { //srcID string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) (*Edge, error) {
	c.edgeMu.Lock()
	defer c.edgeMu.Unlock()

	// Check if edge already exists
	edge, exists := c.edges[regMsg.Msg.EdgeMACAddr]

	if !exists {
		// New edge, allocate an IP address
		//vip, masklen, err := c.addrPool.Request(regMsg.EdgeMACAddr)
		nvip, err := c.ips.RequestIP(regMsg.Msg.EdgeMACAddr, true)
		if err != nil {
			c.debugLog("VIP allocation failed for edge %s: %v", regMsg.Msg.EdgeMACAddr, err)
			return nil, fmt.Errorf("IP allocation failed: %w", err)
		}

		vip, err := netip.ParseAddr(nvip.String())
		if err != nil {
			fmt.Printf("Error converting net.IP to netip.Addr: %v\n", err)
			return nil, err
		}
		// Create new edge
		edge = &Edge{
			Desc:          regMsg.Msg.EdgeDesc,
			PublicIP:      regMsg.FromAddr.IP,
			PublicPort:    regMsg.FromAddr.Port,
			Community:     c.name,
			VirtualIP:     vip,
			VNetMaskLen:   c.maskLen,
			LastHeartbeat: time.Now(),
			LastSequence:  regMsg.Header.Sequence,
			MACAddr:       regMsg.EdgeMACAddr(),
			MachineID:     regMsg.Msg.ClearMachineID,
		}

		c.edges[regMsg.Msg.EdgeMACAddr] = edge
		c.SetEdgeCachedInfo(regMsg.Msg.EdgeMACAddr, edge.Desc, true, edge.VirtualIP)
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
		edge.PublicIP = regMsg.FromAddr.IP
		edge.PublicPort = regMsg.FromAddr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = regMsg.Header.Sequence

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

func (c *Community) GetLease(macAddr string) *ippool.Lease {
	return c.ips.GetLease(macAddr)
}

func (c *Community) IsRegistered(macAddr string) bool {
	c.edgeMu.RLock()
	_, exists := c.edges[macAddr]
	c.edgeMu.RUnlock()
	return exists
}

func (c *Community) GetLeasesWithEdgesInfos() map[string]netstruct.LeaseWithEdgeInfos {
	leases := c.ips.GetAllLeases()
	extLeases := make(map[string]netstruct.LeaseWithEdgeInfos)
	for k, v := range leases {
		extLease := netstruct.LeaseEdgeInfos{
			EdgeID:              "unknown",
			IsRegistered:        false,
			TimeSinceLastUpdate: -(1 * time.Second),
		}
		mac := k
		cachedInfos, exists := c.GetEdgeCachedInfo(mac)
		if exists {
			extLease.EdgeID = cachedInfos.Desc
			extLease.IsRegistered = cachedInfos.IsRegistered
			extLease.TimeSinceLastUpdate = time.Since(cachedInfos.UpdatedAt)
			extLease.VirtualIP = cachedInfos.VirtualIP
		}
		infos := netstruct.LeaseWithEdgeInfos{
			Lease:          v,
			LeaseEdgeInfos: extLease,
		}
		extLeases[k] = infos
	}
	return extLeases
}

func (c *Community) GetAllLease() map[string]ippool.Lease {
	c.edgeMu.RLock()
	defer c.edgeMu.RUnlock()
	leases := make(map[string]ippool.Lease)
	for k := range c.edges {
		l := c.GetLease(k)
		if l == nil {
			continue
		}
		leases[k] = *l
	}
	return leases
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

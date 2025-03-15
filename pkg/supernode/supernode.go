package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/protocol"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds supernode configuration options
type Config struct {
	Debug               bool          // Enable debug logging
	CommunitySubnet     string        // Base subnet for community allocation
	CommunitySubnetCIDR int           // CIDR prefix length for community subnets
	ExpiryDuration      time.Duration // Time after which an edge is considered stale
	CleanupInterval     time.Duration // Interval for the cleanup routine
	UDPBufferSize       int           // Size of UDP socket buffers
	SupportCompact      bool          // Whether to support compact header format
	StrictHashChecking  bool          // Whether to strictly enforce community hash validation
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Debug:               false,
		CommunitySubnet:     "10.128.0.0",
		CommunitySubnetCIDR: 24,
		ExpiryDuration:      10 * time.Minute,
		CleanupInterval:     5 * time.Minute,
		UDPBufferSize:       1024 * 1024, // 1MB buffer
		SupportCompact:      true,        // Enable compact header support by default
		StrictHashChecking:  true,        // Enforce hash validation by default
	}
}

// Edge represents a registered edge.
type Edge struct {
	Desc          string     // from header.SourceID
	PublicIP      net.IP     // Public IP address
	Port          int        // UDP port
	Community     string     // Community name
	VirtualIP     netip.Addr // Virtual IP assigned within the community
	VNetMaskLen   int        // CIDR mask length for the virtual network
	LastHeartbeat time.Time  // Time of last heartbeat
	LastSequence  uint16     // Last sequence number received
	MACAddr       string     // MAC address provided during registration
}

/*
// GlobalID represents a unique identifier for an edge across all communities
type GlobalID struct {
	id        string
	community string
}


// NewGlobalID creates a new GlobalID
func NewGlobalID(id string, community string) *GlobalID {
	return &GlobalID{
		id:        id,
		community: community,
	}
}

// String returns a string representation of a GlobalID
func (gid *GlobalID) String() string {
	return fmt.Sprintf("%s.communities/%s", gid.community, gid.id)
}

// GID returns the GlobalID for an Edge
func (e *Edge) GID() *GlobalID {
	return NewGlobalID(e.ID, e.Community)
}
*/

// SupernodeStats holds runtime statistics
type SupernodeStats struct {
	PacketsProcessed       atomic.Uint64
	PacketsForwarded       atomic.Uint64
	PacketsDropped         atomic.Uint64
	EdgesRegistered        atomic.Uint64
	EdgesUnregistered      atomic.Uint64
	HeartbeatsReceived     atomic.Uint64
	CompactPacketsRecv     atomic.Uint64
	LegacyPacketsRecv      atomic.Uint64
	HashCollisionsDetected atomic.Uint64
	LastCleanupTime        time.Time
	LastCleanupEdges       int
}

// Supernode holds registered edges, VIP pools, and a MAC-to-edge mapping.
type Supernode struct {
	//edgeMu sync.RWMutex       // protects edges
	//edges  map[GlobalID]*Edge // keyed by edge GlobalID

	netAllocator *NetworkAllocator

	comMu       sync.RWMutex
	communities map[uint32]*Community
	//communities map[string]*Community

	// Track community hash-to-name mappings for collision detection
	//hashMu          sync.RWMutex
	//communityHashes map[uint32]string

	//macMu     sync.RWMutex        // protects macToEdge mapping
	//macToEdge map[string]GlobalID // maps normalized MAC string to edge ID

	Conn       *net.UDPConn
	config     *Config
	shutdownCh chan struct{}
	shutdownWg sync.WaitGroup

	// Buffer pool for packet processing
	packetBufPool *buffers.BufferPool

	// Statistics
	stats SupernodeStats

	// Handlers
	SnMessageHandlers protocol.MessageHandlerMap
}

// NewSupernode creates a new Supernode instance with default config
func NewSupernode(conn *net.UDPConn, expiry time.Duration, cleanupInterval time.Duration) *Supernode {
	config := DefaultConfig()
	config.ExpiryDuration = expiry
	config.CleanupInterval = cleanupInterval

	return NewSupernodeWithConfig(conn, config)
}

// NewSupernodeWithConfig creates a new Supernode with the specified configuration
func NewSupernodeWithConfig(conn *net.UDPConn, config *Config) *Supernode {
	// Set UDP buffer sizes
	if err := conn.SetReadBuffer(config.UDPBufferSize); err != nil {
		log.Printf("Warning: couldn't set UDP read buffer size: %v", err)
	}
	if err := conn.SetWriteBuffer(config.UDPBufferSize); err != nil {
		log.Printf("Warning: couldn't set UDP write buffer size: %v", err)
	}

	netAllocator := NewNetworkAllocator(net.ParseIP(config.CommunitySubnet), net.CIDRMask(config.CommunitySubnetCIDR, 32))

	sn := &Supernode{
		netAllocator:      netAllocator,
		communities:       make(map[uint32]*Community),
		Conn:              conn,
		config:            config,
		shutdownCh:        make(chan struct{}),
		packetBufPool:     buffers.PacketBufferPool,
		SnMessageHandlers: make(protocol.MessageHandlerMap),
	}

	sn.SnMessageHandlers[protocol.TypeRegister] = sn.handleRegisterMessage
	sn.SnMessageHandlers[protocol.TypeUnregister] = sn.handleUnregisterMessage
	sn.SnMessageHandlers[protocol.TypeAck] = sn.handleAckMessage
	sn.SnMessageHandlers[protocol.TypeHeartbeat] = sn.handleHeartbeatMessage
	sn.SnMessageHandlers[protocol.TypeData] = sn.handleDataMessage
	sn.shutdownWg.Add(1)
	go func() {
		defer sn.shutdownWg.Done()
		sn.cleanupRoutine()
	}()

	return sn
}

// debugLog logs a message if debug mode is enabled
func (s *Supernode) debugLog(format string, args ...interface{}) {
	if s.config.Debug {
		log.Printf("Supernode DEBUG: "+format, args...)
	}
}

func (s *Supernode) RegisterCommunity(communityName string, communityHash uint32) (*Community, error) {
	chkHash := protocol.HashCommunity(communityName)
	if communityHash != chkHash {
		return nil, fmt.Errorf("wrong hash %d for community name: %s (expected %d)", communityHash, communityName, chkHash)
	}
	s.comMu.RLock()
	cm, exists := s.communities[communityHash]
	s.comMu.RUnlock()
	if exists {
		if cm.Name() == communityName {
			return cm, nil
		}
		return nil, fmt.Errorf("a different communityName already exists for hash %d (from name %s)", communityHash, communityName)
	}
	// Need to create, use write lock
	s.comMu.Lock()
	defer s.comMu.Unlock()

	if exists {
		if cm.Name() == communityName {
			return cm, nil
		}
		return nil, fmt.Errorf("a different communityName already exists for hash %d (from name %s)", communityHash, communityName)
	}

	// Create new community
	prefix, err := s.netAllocator.ProposeVirtualNetwork(communityName)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate network for community %s: %w", communityName, err)
	}

	cm = NewCommunityWithConfig(communityName, prefix, s.config)
	s.communities[communityHash] = cm

	return cm, nil
}

func (s *Supernode) GetCommunityForEdge(edgeMACAddr string, communityHash uint32) (*Community, error) {

	s.comMu.RLock()
	cm, exists := s.communities[communityHash]
	s.comMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("no community found with this hash")
	}

	_, ok := cm.GetEdge(edgeMACAddr)
	if !ok {
		return nil, fmt.Errorf("Community:%s found no registered edge for addr:%s", cm.name, edgeMACAddr)
	}
	return cm, nil

}

// RegisterEdge registers or updates an edge in the supernode
func (s *Supernode) RegisterEdge(regMsg *protocol.RegisterMessage) (*Edge, error) {

	cm, err := s.RegisterCommunity(regMsg.CommunityName, regMsg.CommunityHash)
	if err != nil {
		return nil, err
	}

	// Update edge in community scope
	edge, err := cm.EdgeUpdate(regMsg)
	if err != nil {
		return nil, err
	}

	s.stats.EdgesRegistered.Add(1)

	return edge, nil
}

// UnregisterEdge removes an edge from the supernode
func (s *Supernode) UnregisterEdge(edgeMACAddr string, communityHash uint32) error {

	cm, err := s.GetCommunityForEdge(edgeMACAddr, communityHash)
	if err != nil {
		log.Printf("Supernode: error while unregistering edge %v for community %v: %w", edgeMACAddr, communityHash, err)
		return err
	}

	if cm.Unregister(edgeMACAddr) {
		s.stats.EdgesUnregistered.Add(1)
	}
	return nil
}

// CleanupStaleEdges removes edges that haven't sent a heartbeat within the expiry period
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {

	now := time.Now()
	stales := make(map[*Community][]string)
	var cms []*Community
	s.comMu.RLock()
	for _, cm := range s.communities {
		cms = append(cms, cm)
	}
	s.comMu.RUnlock()
	var totalCleanUp int
	for _, cm := range cms {
		stales[cm] = cm.GetStaleEdgeIDs(expiry)
		totalCleanUp += len(stales[cm])
	}

	for k, vv := range stales {
		for _, id := range vv {
			if k.Unregister(id) {
				log.Printf("Supernode: Removed stale edge %s from community %s", id, k.Name())
				s.stats.EdgesUnregistered.Add(1)
			}
		}
	}

	// Update statistics
	s.stats.LastCleanupTime = now
	s.stats.LastCleanupEdges = totalCleanUp
}

// cleanupRoutine periodically cleans up stale edges
func (s *Supernode) cleanupRoutine() {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CleanupStaleEdges(s.config.ExpiryDuration)
		case <-s.shutdownCh:
			return
		}
	}
}

// formatAckForEdge formats an ACK message, potentially including header format negotiation
func (s *Supernode) formatAckForEdge(edge *Edge, baseAck string) string {
	if !s.config.SupportCompact {
		return baseAck
	}

	// If this is a new registration and we support compact headers, inform the edge
	return baseAck + " COMPACT"
}

// SendAck sends an acknowledgment message to an edge
func (s *Supernode) SendAck(addr *net.UDPAddr, edge *Edge, msg string) error {
	formattedMsg := msg
	if edge != nil {
		formattedMsg = s.formatAckForEdge(edge, msg)
	}

	s.debugLog("Sending ACK to %v: %s", addr, formattedMsg)
	_, err := s.Conn.WriteToUDP([]byte(formattedMsg), addr)
	return err
}

// ProcessPacket processes an incoming packet
func (s *Supernode) ProcessPacket(packet []byte, addr *net.UDPAddr) {
	s.stats.PacketsProcessed.Add(1)

	rawMsg, err := protocol.NewRawMessage(packet, addr)
	if err != nil {
		log.Printf("Supernode: ProcessPacket error: %w", err)
		return
	}

	handler, exists := s.SnMessageHandlers[rawMsg.Header.PacketType]
	if !exists {
		log.Printf("Supernode: Unknown packet type %d from %v", rawMsg.Header.PacketType, rawMsg.Addr)
		s.stats.PacketsDropped.Add(1)
		return
	}
	err = handler(rawMsg)
	if err != nil {
		log.Printf("Supernode: Error from SnMessageHandler: %w", err)
	}

}

func (s *Supernode) handleAckMessage(r *protocol.RawMessage) error {
	ackMessage, err := r.ToAckMessage()
	if err != nil {
		return err
	}
	s.debugLog("Received ACK from edge %s", ackMessage.EdgeMACAddr)
	return nil
}

func (s *Supernode) handleUnregisterMessage(r *protocol.RawMessage) error {
	unRegMsg, err := r.ToUnregisterMessage()
	if err != nil {
		return err
	}
	return s.UnregisterEdge(unRegMsg.EdgeMACAddr, unRegMsg.CommunityHash)
}

func (s *Supernode) handleRegisterMessage(r *protocol.RawMessage) error {
	regMsg, err := r.ToRegisterMessage()
	if err != nil {
		return err
	}
	edge, err := s.RegisterEdge(regMsg)
	if edge == nil || err != nil {
		log.Printf("Supernode: Registration failed for %s: %v", regMsg.EdgeMACAddr, err)
		s.SendAck(r.Addr, nil, "ERR Registration failed")
		s.stats.PacketsDropped.Add(1)
		return err
	}

	ackMsg := fmt.Sprintf("ACK %s %d", edge.VirtualIP.String(), edge.VNetMaskLen)
	s.SendAck(r.Addr, edge, ackMsg)
	return nil
}

func (s *Supernode) handleHeartbeatMessage(r *protocol.RawMessage) error {
	heartbeatMsg, err := r.ToHeartbeatMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(heartbeatMsg.EdgeMACAddr, heartbeatMsg.CommunityHash)
	if err != nil {
		return err
	}
	s.stats.HeartbeatsReceived.Add(1)
	err = cm.RefreshEdge(heartbeatMsg)
	if err != nil {
		return err
	}
	edge, exists := cm.GetEdge(heartbeatMsg.EdgeMACAddr)
	if exists {
		return s.SendAck(r.Addr, edge, "ACK")
	}
	return nil
}

// handleDataMessage processes a data packet
func (s *Supernode) handleDataMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {

	dataMsg, err := r.ToDataMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(dataMsg.EdgeMACAddr, dataMsg.CommunityHash)
	if err != nil {
		return err
	}
	senderEdge, found := cm.GetEdge(dataMsg.EdgeMACAddr)
	if !found {
		return fmt.Errorf("cannot find senderEdge %v in community for data packet handling", dataMsg.EdgeMACAddr)
	}
	var targetEdge *Edge
	if dataMsg.DestMACAddr != "" {
		te, found := cm.GetEdge(dataMsg.DestMACAddr)
		if found {
			targetEdge = te
		} else {

		}
	}
	// Update sender's heartbeat and sequence
	cm.edgeMu.Lock()
	if e, ok := cm.edges[dataMsg.EdgeMACAddr]; ok {
		e.LastHeartbeat = time.Now()
		e.LastSequence = dataMsg.RawMsg.Header.Sequence
	}
	cm.edgeMu.Unlock()

	s.debugLog("Data packet received from edge %s", senderEdge.MACAddr)

	if targetEdge != nil {
		if err := s.forwardPacket(dataMsg.ToPacket(), targetEdge); err != nil {
			s.stats.PacketsDropped.Add(1)
			log.Printf("Supernode: Failed to forward packet to edge %s: %v", targetEdge.MACAddr, err)
		} else {
			s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
			s.stats.PacketsForwarded.Add(1)
			return nil
		}
	}
	s.debugLog("Unable to selectively forward packet orNo destination MAC provided. Broadcasting to community %s", cm.name)
	s.broadcast(dataMsg.ToPacket(), cm, senderEdge.MACAddr)
	return nil
}

// forwardPacket sends a packet to a specific edge
func (s *Supernode) forwardPacket(packet []byte, target *Edge) error {
	addr := &net.UDPAddr{IP: target.PublicIP, Port: target.Port}
	s.debugLog("Forwarding packet to edge %s at %v", target.MACAddr, addr)
	_, err := s.Conn.WriteToUDP(packet, addr)
	return err
}

// broadcast sends a packet to all edges in the same community except the sender
func (s *Supernode) broadcast(packet []byte, cm *Community, senderID string) {
	targets := cm.GetAllEdges()

	// Now send to all targets without holding the lock
	sentCount := 0
	for _, target := range targets {
		if target.MACAddr == senderID {
			continue
		} // Check if we need to convert the packet for this target

		if err := s.forwardPacket(packet, target); err != nil {
			log.Printf("Supernode: Failed to broadcast packet to edge %s: %v", target.MACAddr, err)
			s.stats.PacketsDropped.Add(1)
		} else {
			s.debugLog("Broadcasted packet from %s to edge %s", senderID, target.MACAddr)
			sentCount++
		}
	}

	if sentCount > 0 {
		s.stats.PacketsForwarded.Add(uint64(sentCount))
	}
}

// GetStats returns a copy of the current statistics
func (s *Supernode) GetStats() SupernodeStats {
	return SupernodeStats{
		PacketsProcessed:       atomic.Uint64{},
		PacketsForwarded:       atomic.Uint64{},
		PacketsDropped:         atomic.Uint64{},
		EdgesRegistered:        atomic.Uint64{},
		EdgesUnregistered:      atomic.Uint64{},
		HeartbeatsReceived:     atomic.Uint64{},
		CompactPacketsRecv:     atomic.Uint64{},
		LegacyPacketsRecv:      atomic.Uint64{},
		HashCollisionsDetected: atomic.Uint64{},
		LastCleanupTime:        s.stats.LastCleanupTime,
		LastCleanupEdges:       s.stats.LastCleanupEdges,
	}
}

// Listen begins processing incoming packets
func (s *Supernode) Listen() {
	// Create a worker pool to process packets
	const numWorkers = 4
	packetChan := make(chan packetData, 100)

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		s.shutdownWg.Add(1)
		go func() {
			defer s.shutdownWg.Done()
			for pkt := range packetChan {
				s.ProcessPacket(pkt.data[:pkt.size], pkt.addr)
				s.packetBufPool.Put(pkt.data)
			}
		}()
	}

	// Main receive loop
	go func() {
		defer close(packetChan)

		for {
			select {
			case <-s.shutdownCh:
				return
			default:
				// Get a buffer from the pool
				buf := s.packetBufPool.Get()

				// Read a packet
				n, addr, err := s.Conn.ReadFromUDP(buf)
				if err != nil {
					log.Printf("Supernode: UDP read error: %v", err)
					s.packetBufPool.Put(buf)

					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					continue
				}

				s.debugLog("Received %d bytes from %v", n, addr)

				// Send to worker pool
				packetChan <- packetData{
					data: buf,
					size: n,
					addr: addr,
				}
			}
		}
	}()

	// Block until shutdown
	<-s.shutdownCh
}

// packetData represents a received packet and its metadata
type packetData struct {
	data []byte
	size int
	addr *net.UDPAddr
}

// Shutdown performs a clean shutdown of the supernode
func (s *Supernode) Shutdown() {
	close(s.shutdownCh)
	s.shutdownWg.Wait()
	log.Printf("Supernode: Shutdown complete")
}

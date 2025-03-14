package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/protocol"
	"net"
	"net/netip"
	"strconv"
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
	ID            string     // from header.SourceID
	PublicIP      net.IP     // Public IP address
	Port          int        // UDP port
	Community     string     // Community name
	VirtualIP     netip.Addr // Virtual IP assigned within the community
	VNetMaskLen   int        // CIDR mask length for the virtual network
	LastHeartbeat time.Time  // Time of last heartbeat
	LastSequence  uint16     // Last sequence number received
	MACAddr       string     // MAC address provided during registration
	UseCompact    bool       // Whether this edge uses compact headers
}

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
	edgeMu sync.RWMutex       // protects edges
	edges  map[GlobalID]*Edge // keyed by edge GlobalID

	netAllocator *NetworkAllocator

	comMu       sync.RWMutex
	communities map[string]*Community

	// Track community hash-to-name mappings for collision detection
	hashMu          sync.RWMutex
	communityHashes map[uint32]string

	macMu     sync.RWMutex        // protects macToEdge mapping
	macToEdge map[string]GlobalID // maps normalized MAC string to edge ID

	Conn       *net.UDPConn
	config     *Config
	shutdownCh chan struct{}
	shutdownWg sync.WaitGroup

	// Buffer pool for packet processing
	packetBufPool *buffers.BufferPool

	// Statistics
	stats SupernodeStats
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
		edges:           make(map[GlobalID]*Edge),
		netAllocator:    netAllocator,
		communities:     make(map[string]*Community),
		communityHashes: make(map[uint32]string),
		macToEdge:       make(map[string]GlobalID),
		Conn:            conn,
		config:          config,
		shutdownCh:      make(chan struct{}),
		packetBufPool:   buffers.PacketBufferPool,
	}

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

// GetCommunity retrieves or creates a community
func (s *Supernode) GetCommunity(community string, create bool) (*Community, error) {
	// First try with read lock for better performance
	s.comMu.RLock()
	c, exists := s.communities[community]
	s.comMu.RUnlock()

	if exists {
		return c, nil
	}

	if !create {
		return nil, fmt.Errorf("unknown community: %s", community)
	}

	// Need to create, use write lock
	s.comMu.Lock()
	defer s.comMu.Unlock()

	// Check again in case another goroutine created it while we waited for the lock
	c, exists = s.communities[community]
	if exists {
		return c, nil
	}

	// Create new community
	prefix, err := s.netAllocator.ProposeVirtualNetwork(community)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate network for community %s: %w", community, err)
	}

	c = NewCommunityWithConfig(community, prefix, s.config)
	s.communities[community] = c

	// Register community hash for collision detection
	communityHash := protocol.HashCommunity(community)
	if err := s.registerCommunityHash(community, communityHash); err != nil {
		log.Printf("Warning: %v", err)
		if s.config.StrictHashChecking {
			return nil, err
		}
	}

	return c, nil
}

// registerCommunityHash adds a community hash to the tracking map, checking for collisions
func (s *Supernode) registerCommunityHash(community string, hash uint32) error {
	s.hashMu.Lock()
	defer s.hashMu.Unlock()

	// Check for collision
	if existingCommunity, exists := s.communityHashes[hash]; exists {
		if existingCommunity != community {
			s.stats.HashCollisionsDetected.Add(1)
			return fmt.Errorf("community hash collision detected: %s and %s both hash to %d",
				community, existingCommunity, hash)
		}
	}

	// No collision, register the hash
	s.communityHashes[hash] = community
	return nil
}

// validateCommunityHash checks if a hash matches the expected community
func (s *Supernode) validateCommunityHash(community string, hash uint32) error {
	if !s.config.SupportCompact {
		return nil // Don't validate for legacy mode
	}

	// Verify the hash matches the community
	expectedHash := protocol.HashCommunity(community)
	if hash != expectedHash {
		return fmt.Errorf("community hash mismatch: expected %d, got %d for community %s",
			expectedHash, hash, community)
	}

	// Check for collisions
	s.hashMu.RLock()
	existingCommunity, exists := s.communityHashes[hash]
	s.hashMu.RUnlock()

	if exists && existingCommunity != community {
		s.stats.HashCollisionsDetected.Add(1)
		return fmt.Errorf("community hash collision detected: %s and %s both hash to %d",
			community, existingCommunity, hash)
	}

	return nil
}

// RegisterEdge registers or updates an edge in the supernode
func (s *Supernode) RegisterEdge(srcID, community string, addr *net.UDPAddr, seq uint16, isReg bool, payload string, useCompact bool, communityHash uint32) (*Edge, error) {
	gid := NewGlobalID(srcID, community)

	// For compact headers with registration, validate the community hash
	if useCompact && isReg && s.config.StrictHashChecking {
		if err := s.validateCommunityHash(community, communityHash); err != nil {
			return nil, err
		}
	}

	// Fast path for updates with read lock first
	if !isReg {
		s.edgeMu.RLock()
		_, exists := s.edges[*gid]
		s.edgeMu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("unknown edge with !isReg. Dropping")
		}
	}

	// Get or create the community
	cm, err := s.GetCommunity(community, isReg)
	if err != nil {
		return nil, err
	}

	// Update edge in community scope
	edge, err := cm.EdgeUpdate(srcID, addr, seq, isReg, payload)
	if err != nil {
		return nil, err
	}

	// Update protocol version preference
	if isReg {
		edge.UseCompact = useCompact && s.config.SupportCompact
	}

	// Update global edge map
	s.edgeMu.Lock()
	s.edges[*gid] = edge
	s.edgeMu.Unlock()

	s.macMu.Lock()
	s.macToEdge[edge.MACAddr] = *gid
	s.macMu.Unlock()

	// Update statistics
	if isReg {
		s.stats.EdgesRegistered.Add(1)
	}

	return edge, nil
}

// UnregisterEdge removes an edge from the supernode
func (s *Supernode) UnregisterEdge(srcID string, community string) {
	gid := *NewGlobalID(srcID, community)

	s.edgeMu.Lock()
	edge, exists := s.edges[gid]
	s.edgeMu.Unlock()

	if !exists {
		return
	}

	cm, err := s.GetCommunity(community, false)
	if err != nil {
		log.Printf("Supernode: %v", err)
		return
	}

	if cm.Unregister(srcID) {
		s.macMu.Lock()
		delete(s.macToEdge, edge.MACAddr)
		s.macMu.Unlock()

		s.edgeMu.Lock()
		delete(s.edges, gid)
		s.edgeMu.Unlock()

		// Update statistics
		s.stats.EdgesUnregistered.Add(1)
	}
}

// CleanupStaleEdges removes edges that haven't sent a heartbeat within the expiry period
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	var stale []GlobalID
	now := time.Now()

	// Build list of stale edges with read lock
	s.edgeMu.RLock()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			stale = append(stale, id)
		}
	}
	s.edgeMu.RUnlock()

	// Unregister stale edges outside of the lock
	for _, id := range stale {
		s.UnregisterEdge(id.id, id.community)
		log.Printf("Supernode: Removed stale edge %s from community %s", id.id, id.community)
	}

	// Update statistics
	s.stats.LastCleanupTime = now
	s.stats.LastCleanupEdges = len(stale)
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

	if len(packet) < protocol.CompactHeaderSize {
		log.Printf("Supernode: Packet too short from %v", addr)
		s.stats.PacketsDropped.Add(1)
		return
	}

	// First check the version byte to determine header format
	version := packet[0]

	var hdr protocol.IHeader
	var headerSize int
	var isCompact bool
	var communityHash uint32

	switch version {
	case protocol.VersionLegacy:
		// Legacy header format
		if len(packet) < protocol.TotalHeaderSize {
			log.Printf("Supernode: Packet too short for legacy header from %v", addr)
			s.stats.PacketsDropped.Add(1)
			return
		}

		var legacyHeader protocol.Header
		if err := legacyHeader.UnmarshalBinary(packet[:protocol.TotalHeaderSize]); err != nil {
			log.Printf("Supernode: Failed to unmarshal legacy header from %v: %v", addr, err)
			s.stats.PacketsDropped.Add(1)
			return
		}

		hdr = &legacyHeader
		headerSize = protocol.TotalHeaderSize
		isCompact = false
		s.stats.LegacyPacketsRecv.Add(1)

	case protocol.VersionCompact:
		// Compact header format
		if !s.config.SupportCompact {
			log.Printf("Supernode: Compact header received but not supported, from %v", addr)
			s.stats.PacketsDropped.Add(1)
			return
		}

		var compactHeader protocol.CompactHeader
		if err := compactHeader.UnmarshalBinary(packet[:protocol.CompactHeaderSize]); err != nil {
			log.Printf("Supernode: Failed to unmarshal compact header from %v: %v", addr, err)
			s.stats.PacketsDropped.Add(1)
			return
		}

		hdr = &compactHeader
		headerSize = protocol.CompactHeaderSize
		isCompact = true
		communityHash = compactHeader.CommunityID
		s.stats.CompactPacketsRecv.Add(1)

	default:
		log.Printf("Supernode: Unknown header version %d from %v", version, addr)
		s.stats.PacketsDropped.Add(1)
		return
	}

	// Extract common header information
	community := hdr.GetCommunity()
	srcID := hdr.GetSourceID()
	packetType := hdr.GetPacketType()
	seq := hdr.GetSequence()

	// Get destination MAC if available
	var destMAC string
	if mac := hdr.GetDestinationMAC(); mac != nil {
		destMAC = mac.String()
	}

	// Extract payload based on header size
	payload := packet[headerSize:]

	s.debugLog("Received packet: Type=%d, srcID=%q, destMAC=%q, community=%q, seq=%d, payloadLen=%d, isCompact=%v",
		packetType, srcID, destMAC, community, seq, len(payload), isCompact)

	switch packetType {
	case protocol.TypeRegister:
		s.handleRegister(srcID, community, addr, seq, string(payload), isCompact, communityHash)
	case protocol.TypeUnregister:
		s.UnregisterEdge(srcID, community)
	case protocol.TypeHeartbeat:
		s.handleHeartbeat(srcID, community, addr, seq, isCompact, communityHash)
	case protocol.TypeData:
		s.handleData(packet, srcID, community, destMAC, seq)
	case protocol.TypeAck:
		s.debugLog("Received ACK from edge %s", srcID)
	default:
		log.Printf("Supernode: Unknown packet type %d from %v", packetType, addr)
		s.stats.PacketsDropped.Add(1)
	}
}

// handleRegister processes a registration packet
func (s *Supernode) handleRegister(srcID, community string, addr *net.UDPAddr, seq uint16, payload string, isCompact bool, communityHash uint32) {
	payloadStr := strings.TrimSpace(payload)

	// For compact headers, parse community hash from payload if present
	if isCompact && strings.HasPrefix(payloadStr, "REGISTER") {
		parts := strings.Fields(payloadStr)
		// Format: REGISTER <edgeID> <MAC> <community> <communityHash>
		if len(parts) >= 5 {
			hashFromPayload, err := strconv.ParseUint(parts[4], 10, 32)
			if err == nil {
				communityHash = uint32(hashFromPayload)
			}
		}
	}

	edge, err := s.RegisterEdge(srcID, community, addr, seq, true, payloadStr, isCompact, communityHash)

	if edge == nil || err != nil {
		log.Printf("Supernode: Registration failed for %s: %v", srcID, err)
		s.SendAck(addr, nil, "ERR Registration failed")
		s.stats.PacketsDropped.Add(1)
		return
	}

	ackMsg := fmt.Sprintf("ACK %s %d", edge.VirtualIP.String(), edge.VNetMaskLen)
	s.SendAck(addr, edge, ackMsg)
}

// handleHeartbeat processes a heartbeat packet
func (s *Supernode) handleHeartbeat(srcID, community string, addr *net.UDPAddr, seq uint16, isCompact bool, communityHash uint32) {
	edge, err := s.RegisterEdge(srcID, community, addr, seq, false, "", isCompact, communityHash)
	if edge != nil && err == nil {
		s.debugLog("Heartbeat from edge: id=%s, community=%s, VIP=%s, MAC=%s",
			srcID, edge.Community, edge.VirtualIP.String(), edge.MACAddr)

		// Update statistics
		s.stats.HeartbeatsReceived.Add(1)
	}
	s.SendAck(addr, edge, "ACK")
}

// handleData processes a data packet
func (s *Supernode) handleData(packet []byte, srcID, community, destMAC string, seq uint16) {
	gid := *NewGlobalID(srcID, community)

	// Look up the sender edge with read lock
	s.edgeMu.RLock()
	senderEdge, exists := s.edges[gid]
	s.edgeMu.RUnlock()

	if !exists {
		log.Printf("Supernode: Received data packet from unregistered edge %s; dropping packet", srcID)
		s.stats.PacketsDropped.Add(1)
		return
	}

	// Update sender's heartbeat and sequence
	s.edgeMu.Lock()
	senderEdge.LastHeartbeat = time.Now()
	senderEdge.LastSequence = seq
	s.edgeMu.Unlock()

	s.debugLog("Data packet received from edge %s", srcID)

	// Try to deliver to specific destination if MAC is provided
	if destMAC != "" {
		delivered := s.tryDeliverToMAC(packet, destMAC, community, srcID)
		if !delivered {
			s.debugLog("Destination MAC %s not found. Broadcasting to community %s", destMAC, community)
			s.broadcast(packet, community, srcID)
		}
	} else {
		s.debugLog("No destination MAC provided. Broadcasting to community %s", community)
		s.broadcast(packet, community, srcID)
	}
}

// tryDeliverToMAC attempts to deliver a packet to a specific MAC address
func (s *Supernode) tryDeliverToMAC(packet []byte, destMAC, community, srcID string) bool {
	// Get target edge ID from MAC
	s.macMu.RLock()
	targetEdgeID, exists := s.macToEdge[destMAC]
	s.macMu.RUnlock()

	if !exists {
		return false
	}

	// Get target edge
	s.edgeMu.RLock()
	target, ok := s.edges[targetEdgeID]
	s.edgeMu.RUnlock()

	if !ok {
		// Edge not found, possibly stale MAC mapping
		s.macMu.Lock()
		if id, exists := s.macToEdge[destMAC]; exists && id == targetEdgeID {
			delete(s.macToEdge, destMAC)
		}
		s.macMu.Unlock()
		return false
	}

	// Check if we need to convert the packet format for the target edge
	needConvert := false

	// Get header version (first byte)
	version := packet[0]

	// If sender uses legacy but target uses compact, or vice versa, we need to convert
	if (version == protocol.VersionLegacy && target.UseCompact) ||
		(version == protocol.VersionCompact && !target.UseCompact) {
		needConvert = true
	}

	// Convert the packet format if needed
	if needConvert {
		convertedPacket, err := s.convertPacketFormat(packet, target.UseCompact)
		if err != nil {
			log.Printf("Supernode: Failed to convert packet format: %v", err)
			s.stats.PacketsDropped.Add(1)
			return false
		}
		packet = convertedPacket
	}

	// Forward packet to the target
	if err := s.forwardPacket(packet, target); err != nil {
		log.Printf("Supernode: Failed to forward packet to edge %s: %v", target.ID, err)
		s.stats.PacketsDropped.Add(1)
		return false
	}

	s.debugLog("Forwarded packet to edge %s", target.ID)
	s.stats.PacketsForwarded.Add(1)
	return true
}

// convertPacketFormat converts between legacy and compact header formats
func (s *Supernode) convertPacketFormat(packet []byte, toCompact bool) ([]byte, error) {
	// Parse the source header
	sourceHeader, err := protocol.ParseHeader(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source header: %w", err)
	}

	var targetHeaderSize int
	var targetHeader protocol.IHeader

	// Get payload based on source header type
	var payload []byte

	switch h := sourceHeader.(type) {
	case *protocol.Header:
		payload = packet[protocol.TotalHeaderSize:]
		if toCompact {
			// Convert Legacy -> Compact
			targetHeader = protocol.ConvertToCompactHeader(h)
			targetHeaderSize = protocol.CompactHeaderSize
		} else {
			// No conversion needed
			return packet, nil
		}

	case *protocol.CompactHeader:
		payload = packet[protocol.CompactHeaderSize:]
		if !toCompact {
			// Convert Compact -> Legacy
			targetHeader = protocol.ConvertToLegacyHeader(h)
			targetHeaderSize = protocol.TotalHeaderSize
		} else {
			// No conversion needed
			return packet, nil
		}

	default:
		return nil, fmt.Errorf("unknown header type")
	}

	// Create new packet with converted header
	newPacket := make([]byte, targetHeaderSize+len(payload))

	// Marshal new header
	if h, ok := targetHeader.(*protocol.Header); ok {
		if err := h.MarshalBinaryTo(newPacket[:protocol.TotalHeaderSize]); err != nil {
			return nil, fmt.Errorf("failed to marshal legacy header: %w", err)
		}
	} else if h, ok := targetHeader.(*protocol.CompactHeader); ok {
		if err := h.MarshalBinaryTo(newPacket[:protocol.CompactHeaderSize]); err != nil {
			return nil, fmt.Errorf("failed to marshal compact header: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unknown target header type")
	}

	// Copy payload
	copy(newPacket[targetHeaderSize:], payload)

	return newPacket, nil
}

// forwardPacket sends a packet to a specific edge
func (s *Supernode) forwardPacket(packet []byte, target *Edge) error {
	addr := &net.UDPAddr{IP: target.PublicIP, Port: target.Port}
	s.debugLog("Forwarding packet to edge %s at %v", target.ID, addr)
	_, err := s.Conn.WriteToUDP(packet, addr)
	return err
}

// broadcast sends a packet to all edges in the same community except the sender
func (s *Supernode) broadcast(packet []byte, community, senderID string) {
	// First get a list of targets to avoid holding the lock during sends
	var targets []*Edge

	s.edgeMu.RLock()
	for _, edge := range s.edges {
		if edge.Community == community && edge.ID != senderID {
			// Make a copy to avoid race conditions
			edgeCopy := *edge
			targets = append(targets, &edgeCopy)
		}
	}
	s.edgeMu.RUnlock()

	// First byte indicates the packet's header format
	version := packet[0]

	// Now send to all targets without holding the lock
	sentCount := 0
	for _, target := range targets {
		// Check if we need to convert the packet for this target
		needConvert := false
		if (version == protocol.VersionLegacy && target.UseCompact) ||
			(version == protocol.VersionCompact && !target.UseCompact) {
			needConvert = true
		}

		packetToSend := packet

		if needConvert {
			converted, err := s.convertPacketFormat(packet, target.UseCompact)
			if err != nil {
				log.Printf("Supernode: Failed to convert packet for broadcast to edge %s: %v", target.ID, err)
				s.stats.PacketsDropped.Add(1)
				continue
			}
			packetToSend = converted
		}

		if err := s.forwardPacket(packetToSend, target); err != nil {
			log.Printf("Supernode: Failed to broadcast packet to edge %s: %v", target.ID, err)
			s.stats.PacketsDropped.Add(1)
		} else {
			s.debugLog("Broadcasted packet from %s to edge %s", senderID, target.ID)
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

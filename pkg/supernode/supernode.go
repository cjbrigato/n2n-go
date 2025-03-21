package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

/*
// Config holds supernode configuration options
type Config struct {
	Debug               bool          // Enable debug logging
	CommunitySubnet     string        // Base subnet for community allocation
	CommunitySubnetCIDR int           // CIDR prefix length for community subnets
	ExpiryDuration      time.Duration // Time after which an edge is considered stale
	CleanupInterval     time.Duration // Interval for the cleanup routine
	UDPBufferSize       int           // Size of UDP socket buffers
	StrictHashChecking  bool          // Whether to strictly enforce community hash validation
	EnableVFuze         bool
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
		StrictHashChecking:  true,        // Enforce hash validation by default
		EnableVFuze:         true,
	}
}
*/

// Supernode holds registered edges, VIP pools, and a MAC-to-edge mapping.
type Supernode struct {
	netAllocator *NetworkAllocator

	comMu       sync.RWMutex
	communities map[uint32]*Community

	edgeMu        sync.RWMutex
	edgesByMAC    map[string]*Edge
	edgesBySocket map[string]*Edge // UDP.Addr(String)

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

func (s *Supernode) MacADDR() net.HardwareAddr {
	mac, _ := net.ParseMAC("CA:FE:C0:FF:EE:00")
	return mac
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
		edgesByMAC:        map[string]*Edge{},
		edgesBySocket:     map[string]*Edge{},
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
	sn.SnMessageHandlers[protocol.TypePeerRequest] = sn.handlePeerRequestMessage
	sn.SnMessageHandlers[protocol.TypePing] = sn.handlePingMessage
	sn.SnMessageHandlers[protocol.TypeP2PStateInfo] = sn.handleP2PStateInfoMessage
	sn.SnMessageHandlers[protocol.TypeP2PFullState] = sn.handleP2PFullStateMessage

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
func (s *Supernode) RegisterEdge(regMsg *protocol.RegisterMessage) (*Edge, *Community, error) {

	cm, err := s.RegisterCommunity(regMsg.CommunityName, regMsg.CommunityHash)
	if err != nil {
		return nil, nil, err
	}

	// Update edge in community scope
	edge, err := cm.EdgeUpdate(regMsg)
	if err != nil {
		return nil, nil, err
	}

	s.stats.EdgesRegistered.Add(1)

	return edge, cm, nil
}

func (s *Supernode) onEdgeUnregistered(cm *Community, edgeMACAddr string) {
	s.edgeMu.Lock()
	edge := s.edgesByMAC[edgeMACAddr]
	pil := newPeerInfoEvent(p2p.TypeUnregister, edge)
	delete(s.edgesBySocket, edge.UDPAddr().String())
	delete(s.edgesByMAC, edgeMACAddr)
	s.edgeMu.Unlock()
	s.stats.EdgesUnregistered.Add(1)
	peerInfoPayload, err := pil.Encode()
	if err != nil {
		log.Printf("Supernode: (warn) unable to send unregistration event to peers for community %s: %v", cm.Name(), err)
	} else {
		s.BroadcastPacket(protocol.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), edgeMACAddr)
	}
}

// UnregisterEdge removes an edge from the supernode
func (s *Supernode) UnregisterEdge(edgeMACAddr string, communityHash uint32) error {

	cm, err := s.GetCommunityForEdge(edgeMACAddr, communityHash)
	if err != nil {
		log.Printf("Supernode: error while unregistering edge %v for community %v: %w", edgeMACAddr, communityHash, err)
		return err
	}

	if cm.Unregister(edgeMACAddr) {
		s.onEdgeUnregistered(cm, edgeMACAddr)
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
				s.onEdgeUnregistered(k, id)
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

// SendAck sends an acknowledgment message to an edge
func (s *Supernode) SendAck(addr *net.UDPAddr, edge *Edge, msg string) error {
	formattedMsg := msg
	if edge != nil {
		formattedMsg = msg
	}

	s.debugLog("Sending ACK to %v: %s", addr, formattedMsg)
	_, err := s.Conn.WriteToUDP([]byte(formattedMsg), addr)
	return err
}

func (s *Supernode) handleVFuze(packet []byte) {
	dst := net.HardwareAddr(packet[1:7])
	s.edgeMu.Lock()
	edge, ok := s.edgesByMAC[dst.String()]
	s.edgeMu.Unlock()
	if s.config.EnableVFuze {
		if ok {
			err := s.forwardPacket(packet, edge)
			if err != nil {
				log.Printf("Supernode: VersionVFuze error: %v", err)
			}
		} else {
			log.Printf("Supernode: cannot process VFuze packet: no edge found with this HardwareAddr %s", dst.String())
		}
	} else {
		log.Printf("Supernode: received a VFuze Protocol packet but configuration disabled it")
		if !ok {
			log.Printf("         -> cannot give hint about which edge has misconfiguration")
			log.Printf("           -> no edge found with requested HardwareAddr routing")
		} else {
			log.Printf("          -> Misconfigured edge Informations: Desc=%s, VIP=%s, MAC=%s", edge.Desc, edge.VirtualIP, edge.MACAddr)
		}
	}
}

// ProcessPacket processes an incoming packet
func (s *Supernode) ProcessPacket(packet []byte, addr *net.UDPAddr) {

	s.stats.PacketsProcessed.Add(1)
	if packet[0] == protocol.VersionVFuze {
		s.handleVFuze(packet)
		return
	}

	rawMsg, err := protocol.NewRawMessage(packet, addr)
	if err != nil {
		log.Printf("Supernode: ProcessPacket error: %v", err)
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
		log.Printf("Supernode: Error from SnMessageHandler[%s]: %v", rawMsg.Header.PacketType.String(), err)
	}

}

func newPeerInfoEvent(eventType p2p.PeerInfoEventType, edge *Edge) *p2p.PeerInfoList {
	return &p2p.PeerInfoList{
		PeerInfos: []p2p.PeerInfo{
			edge.PeerInfo(),
		},
		EventType: eventType,
	}
}

func (s *Supernode) handleP2PStateInfoMessage(r *protocol.RawMessage) error {
	p2pMsg, err := r.ToP2PStateInfoMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(p2pMsg.EdgeMACAddr, p2pMsg.CommunityHash)
	if err != nil {
		return err
	}
	err = cm.SetP2PInfosFor(p2pMsg.EdgeMACAddr, p2pMsg.PeerP2PInfos)
	if err != nil {
		return err
	}
	s.debugLog("Community:%s updated P2PInfosFor:%s", cm.Name(), p2pMsg.EdgeMACAddr)
	return nil
}

func (s *Supernode) handlePeerRequestMessage(r *protocol.RawMessage) error {
	peerReqMsg, err := r.ToPeerRequestMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(peerReqMsg.EdgeMACAddr, peerReqMsg.CommunityHash)
	if err != nil {
		return err
	}
	pil := cm.GetPeerInfoList(peerReqMsg.EdgeMACAddr, true)
	peerResponsePayload, err := pil.Encode()
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(peerReqMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	return s.WritePacket(protocol.TypePeerInfo, peerReqMsg.CommunityName, s.MacADDR(), nil, string(peerResponsePayload), target)
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
	edge, cm, err := s.RegisterEdge(regMsg)
	if edge == nil || err != nil {
		log.Printf("Supernode: Registration failed for %s: %v", regMsg.EdgeMACAddr, err)
		s.SendAck(r.Addr, nil, "ERR Registration failed")
		s.stats.PacketsDropped.Add(1)
		return err
	}
	s.edgeMu.Lock()
	s.edgesByMAC[edge.MACAddr] = edge
	s.edgesBySocket[edge.UDPAddr().String()] = edge
	s.edgeMu.Unlock()
	pil := newPeerInfoEvent(p2p.TypeRegister, edge)
	peerInfoPayload, err := pil.Encode()
	if err != nil {
		log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
	} else {
		s.BroadcastPacket(protocol.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), regMsg.EdgeMACAddr)
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
	changed, err := cm.RefreshEdge(heartbeatMsg)
	if err != nil {
		return err
	}
	edge, exists := cm.GetEdge(heartbeatMsg.EdgeMACAddr)
	if exists {
		if changed {
			pil := newPeerInfoEvent(p2p.TypeRegister, edge)
			peerInfoPayload, err := pil.Encode()
			if err != nil {
				log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
			} else {
				s.BroadcastPacket(protocol.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), heartbeatMsg.EdgeMACAddr)
			}
		}
		return s.SendAck(r.Addr, edge, "ACK")
	}
	return nil
}

func (s *Supernode) handleP2PFullStateMessage(r *protocol.RawMessage) error {
	fsMsg, err := r.ToP2PFullStateMessage()
	if err != nil {
		return err
	}
	if !fsMsg.IsRequest {
		return fmt.Errorf("supernode does not handle non-requests P2PFullStatesMessage")
	}
	cm, err := s.GetCommunityForEdge(fsMsg.EdgeMACAddr, fsMsg.CommunityHash)
	if err != nil {
		return err
	}
	P2PFullState, err := cm.GetCommunityPeerP2PInfosDatas(fsMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	data, err := P2PFullState.Encode()
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(fsMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	//log.Printf("DEBUG: P2PFullStateMessageSize: %d bytes", len(string(data)))
	//return s.WriteFragments(protocol.TypeP2PFullState, s.MacADDR(), data, target)
	return s.WritePacket(protocol.TypeP2PFullState, cm.Name(), s.MacADDR(), nil, string(data), target)

}

// handleDataMessage processes a data packet
func (s *Supernode) handlePingMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {

	pingMsg, err := r.ToPingMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(pingMsg.EdgeMACAddr, pingMsg.CommunityHash)
	if err != nil {
		return err
	}
	senderEdge, found := cm.GetEdge(pingMsg.EdgeMACAddr)
	if !found {
		return fmt.Errorf("cannot find senderEdge %v in community for pingMsg packet handling", pingMsg.EdgeMACAddr)
	}
	var targetEdge *Edge
	if pingMsg.DestMACAddr != "" {
		te, found := cm.GetEdge(pingMsg.DestMACAddr)
		if found {
			targetEdge = te
		} else {

		}
	}
	// Update sender's heartbeat and sequence
	cm.edgeMu.Lock()
	if e, ok := cm.edges[pingMsg.EdgeMACAddr]; ok {
		e.LastHeartbeat = time.Now()
		e.LastSequence = pingMsg.RawMsg.Header.Sequence
	}
	cm.edgeMu.Unlock()

	s.debugLog("Ping packet received from edge %s", senderEdge.MACAddr)

	if targetEdge != nil {
		if err := s.forwardPacket(pingMsg.ToPacket(), targetEdge); err != nil {
			s.stats.PacketsDropped.Add(1)
			log.Printf("Supernode: Failed to forward pingMessage to edge %s: %v", targetEdge.MACAddr, err)
		} else {
			s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
			s.stats.PacketsForwarded.Add(1)
			return nil
		}
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
	addr := target.UDPAddr()
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

func (s *Supernode) WritePacket(pt protocol.PacketType, community string, src, dst net.HardwareAddr, payloadStr string, addr *net.UDPAddr) error {
	// Get buffer for full packet
	packetBuf := s.packetBufPool.Get()
	defer s.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		protocol.VersionV,
		64,
		pt,
		0,
		community,
		src,
		dst,
	)
	if err != nil {
		return err
	}

	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("Supernode: failed to protov %s header: %w", pt.String(), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], []byte(payloadStr))
	totalLen = protocol.ProtoVHeaderSize + payloadLen

	// Send the packet
	_, err = s.Conn.WriteToUDP(packetBuf[:totalLen], addr)
	if err != nil {
		return fmt.Errorf("edge: failed to send packet: %w", err)
	}
	return nil
}

/*
func (s *Supernode) WriteFragments(pt protocol.PacketType, src net.HardwareAddr, payload []byte, addr *net.UDPAddr) error {
	frags := protocol.MakeVFragPackets(pt, src, payload)
	for _, f := range frags {
		_, err := s.Conn.WriteToUDP(f, addr)
		if err != nil {
			return fmt.Errorf("Supernode: failed to send Fragments: %w", err)
		}
	}
	return nil
}
*/

func (s *Supernode) BroadcastPacket(pt protocol.PacketType, cm *Community, src, dst net.HardwareAddr, payloadStr string, senderMac string) error {
	// Get buffer for full packet
	packetBuf := s.packetBufPool.Get()
	defer s.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		protocol.VersionV,
		64,
		pt,
		0,
		cm.Name(),
		src,
		dst,
	)
	if err != nil {
		return err
	}

	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("Supernode: failed to protov %s header: %w", pt.String(), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], []byte(payloadStr))
	totalLen = protocol.ProtoVHeaderSize + payloadLen

	s.broadcast(packetBuf[:totalLen], cm, senderMac)
	return nil
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
	const numWorkers = 8
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

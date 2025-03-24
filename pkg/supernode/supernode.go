package supernode

import (
	"errors"
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/machine"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrCommunityUnknownEdge = errors.New("found no registered edge")
	ErrCommunityNotFound    = errors.New("no community found with this hash")
)

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

	SNSecrets *crypto.SNSecrets
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

	log.Printf("Generating secrets...")
	secrets, err := crypto.GenSNSecrets()
	if err != nil {
		log.Fatalf("could not generate secrets: %v", err)
	}

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
		SNSecrets:         secrets,
	}

	sn.SnMessageHandlers[spec.TypeRegisterRequest] = sn.handleRegisterMessage
	sn.SnMessageHandlers[spec.TypeUnregisterRequest] = sn.handleUnregisterMessage
	sn.SnMessageHandlers[spec.TypeHeartbeat] = sn.handleHeartbeatMessage
	sn.SnMessageHandlers[spec.TypeData] = sn.handleDataMessage
	sn.SnMessageHandlers[spec.TypePeerListRequest] = sn.handlePeerRequestMessage
	sn.SnMessageHandlers[spec.TypePing] = sn.handlePingMessage
	sn.SnMessageHandlers[spec.TypeP2PStateInfo] = sn.handleP2PStateInfoMessage
	sn.SnMessageHandlers[spec.TypeP2PFullState] = sn.handleP2PFullStateMessage
	sn.SnMessageHandlers[spec.TypeLeasesInfos] = sn.handleLeasesInfosMessage
	sn.SnMessageHandlers[spec.TypeSNPublicSecret] = sn.handleSNPublicSecretMessage

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

	cm, err = NewCommunityWithConfig(communityName, prefix, s.config)
	if err != nil {
		return nil, err
	}
	s.communities[communityHash] = cm

	return cm, nil
}

func (s *Supernode) ValidateEdgeClaimedMACAddr(reg *protocol.Message[*netstruct.RegisterRequest]) ([]byte, error) {

	claimedMAC := reg.Msg.EdgeMACAddr
	encMachineID := reg.Msg.EncryptedMachineID
	machineID, err := crypto.DecryptSequence(encMachineID, s.SNSecrets.Priv)
	if err != nil {
		return nil, err
	}
	computedMAC, err := machine.ExtGenerateMac(reg.Msg.CommunityName, machineID)
	if err != nil {
		return nil, err
	}
	computedClaimableMAC := computedMAC.String()
	if computedClaimableMAC != claimedMAC {
		return nil, fmt.Errorf("computedMac from shared secret (%s) doesn't match claimed mac (%s)", computedClaimableMAC, claimedMAC)
	}
	return machineID, nil
}

func (s *Supernode) GetCommunityForEdge(edgeMACAddr string, communityHash uint32) (*Community, error) {

	s.comMu.RLock()
	cm, exists := s.communities[communityHash]
	s.comMu.RUnlock()
	if !exists {
		return nil, ErrCommunityNotFound
	}

	_, ok := cm.GetEdge(edgeMACAddr)
	if !ok {
		return nil, fmt.Errorf("Community:%s addr %s: %w", cm.name, edgeMACAddr, ErrCommunityUnknownEdge)
	}
	return cm, nil

}

func (s *Supernode) GetCommunity(i EdgeAddressable) (*Community, error) {
	return s.GetCommunityForEdge(i.EdgeMACAddr(), i.CommunityHash())
}

// RegisterEdge registers or updates an edge in the supernode
func (s *Supernode) RegisterEdge(regMsg *protocol.Message[*netstruct.RegisterRequest]) (*Edge, *Community, error) {

	machineID, err := s.ValidateEdgeClaimedMACAddr(regMsg)
	if err != nil {
		return nil, nil, err
	}
	regMsg.Msg.ClearMachineID = machineID

	cm, err := s.RegisterCommunity(regMsg.Msg.CommunityName, regMsg.CommunityHash())
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
	/*peerInfoPayload, err := pil.Encode()
	if err != nil {
		log.Printf("Supernode: (warn) unable to send unregistration event to peers for community %s: %v", cm.Name(), err)
	} else {*/
	s.BroadcastStruct(pil, cm, s.MacADDR(), nil, edgeMACAddr)
	//s.BroadcastPacket(spec.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), edgeMACAddr)
	//}
}

// UnregisterEdge removes an edge from the supernode
func (s *Supernode) UnregisterEdge(ea EdgeAddressable) error {

	cm, err := s.GetCommunity(ea)
	if err != nil {
		log.Printf("Supernode: error while unregistering edge %v for community %v: %w", ea.EdgeMACAddr(), ea.CommunityHash(), err)
		return err
	}

	if cm.Unregister(ea.EdgeMACAddr()) {
		s.onEdgeUnregistered(cm, ea.EdgeMACAddr())
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
		log.Printf("Supernode: Unknown packet type %d from %v", rawMsg.Header.PacketType, rawMsg.FromAddr)
		s.stats.PacketsDropped.Add(1)
		return
	}
	err = handler(rawMsg)
	if err != nil {
		log.Printf("Supernode: Error from SnMessageHandler[%s]: %v", rawMsg.Header.PacketType.String(), err)
		if errors.Is(err, ErrCommunityUnknownEdge) || errors.Is(err, ErrCommunityNotFound) {
			log.Printf("Supernode: sending RetryRegisterRequest to addr:%s", addr.IP)
			s.WritePacket(spec.TypeRetryRegisterRequest, "", s.MacADDR(), nil, "", addr)
		}
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

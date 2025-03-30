package supernode

import (
	"errors"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/spec"
	"net"
	"strings"
	"sync"
	"time"
)

// Supernode holds registered edges, VIP pools, and a MAC-to-edge mapping.
type Supernode struct {
	netAllocator *NetworkAllocator

	comMu       sync.RWMutex
	communities map[uint32]*Community

	edgeMu        sync.RWMutex
	edgesByMAC    map[string]*Edge
	edgesBySocket map[string]*Edge // UDP.Addr(String)

	edgeCacheMu     sync.RWMutex
	edgeCachedInfos map[string]*EdgeCachedInfos // keyed by MacADDR, only overwrites

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
		edgeCachedInfos:   make(map[string]*EdgeCachedInfos),
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

	err = s.SnMessageHandlers.Handle(rawMsg)
	if err != nil {
		if errors.Is(err, ErrUnicastForwardFail) {
			s.debugLog("Supernode: Error from SnMessageHandler[%s]: %v", rawMsg.Header.PacketType.String(), err)
		} else {
			log.Printf("Supernode: Error from SnMessageHandler[%s]: %v", rawMsg.Header.PacketType.String(), err)
		}
		if errors.Is(err, ErrCommunityUnknownEdge) || errors.Is(err, ErrCommunityNotFound) {
			log.Printf("Supernode: sending RetryRegisterRequest to addr:%s", addr.IP)
			s.WritePacket(spec.TypeRetryRegisterRequest, "", nil, nil, addr)
		}
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

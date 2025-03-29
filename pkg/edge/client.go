package edge

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/machine"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/spec"
	"n2n-go/pkg/tuntap"
	"n2n-go/pkg/upnp"
	"n2n-go/pkg/util"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	Peers *p2p.PeerRegistry

	ID            string
	Community     string
	SupernodeAddr *net.UDPAddr
	Conn          *net.UDPConn
	TAP           *tuntap.Interface
	seq           uint32

	EncryptionKey     []byte
	encryptionEnabled bool

	protocolVersion   uint8
	heartbeatInterval time.Duration
	verifyHash        bool
	enableVFuze       bool
	communityHash     uint32

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	IgdClient *upnp.UPnPClient

	VirtualIP string
	MACAddr   net.HardwareAddr

	machineId      []byte
	predictableMac net.HardwareAddr

	fragMu sync.RWMutex
	frag   map[string]map[uint8][]byte

	unregisterOnce sync.Once
	running        atomic.Bool

	// Buffer pools
	packetBufPool *buffers.BufferPool
	headerBufPool *buffers.BufferPool

	// Stats
	PacketsSent atomic.Uint64
	PacketsRecv atomic.Uint64

	EAPI *EdgeClientApi

	SNPubKey *rsa.PublicKey

	//state
	registered bool
	config     *Config
	// Handlers
	messageHandlers protocol.MessageHandlerMap

	isWaitingForSNPubKeyUpdate          bool
	isWaitingForSNRetryRegisterResponse bool
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(cfg Config) (*EdgeClient, error) {

	machineId, err := machine.GetMachineID()
	if err != nil {
		return nil, err
	}
	log.Printf("Edge: got machine-id: %s", hex.EncodeToString(machineId))
	predictableMac, err := machine.GenerateMac(cfg.Community)
	if err != nil {
		return nil, err
	}
	log.Printf("Edge: %s TAP ifName machine-id based Community %s MAC Address: %s", cfg.TapName, cfg.Community, predictableMac.String())

	conn, tap, snAddr, err := setupNetworkComponents(cfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	communityHash := protocol.HashCommunity(cfg.Community)
	igdClient := SetupUPnP(conn, cfg.EdgeID)

	err = util.IfMac(tap.Name(), predictableMac.String())
	if err != nil {
		log.Fatalf("err: %v", err)
	}

	encryptionEnabled := false
	var encryptionKey []byte
	if cfg.EncryptionPassphrase != "" {
		log.Printf("Edge: Attention! Encryption of data packets payload is enabled. Ensure all edge for the community uses same passphrase !")
		encryptionEnabled = true
		encryptionKey = crypto.KeyFromPassphrase(cfg.EncryptionPassphrase)
	}

	edge := &EdgeClient{
		Peers:             p2p.NewPeerRegistry(cfg.Community),
		ID:                cfg.EdgeID,
		Community:         cfg.Community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		seq:               0,
		IgdClient:         igdClient,
		MACAddr:           tap.HardwareAddr(),
		predictableMac:    predictableMac,
		machineId:         machineId,
		protocolVersion:   cfg.ProtocolVersion,
		heartbeatInterval: cfg.HeartbeatInterval,
		verifyHash:        cfg.VerifyHash,
		enableVFuze:       cfg.EnableVFuze,
		communityHash:     communityHash,
		ctx:               ctx,
		cancel:            cancel,
		packetBufPool:     buffers.PacketBufferPool,
		headerBufPool:     buffers.HeaderBufferPool,
		messageHandlers:   make(protocol.MessageHandlerMap),
		config:            &cfg,
		encryptionEnabled: encryptionEnabled,
		EncryptionKey:     encryptionKey,
	}
	edge.messageHandlers[spec.TypeData] = edge.handleDataMessage
	edge.messageHandlers[spec.TypePeerInfo] = edge.handlePeerInfoMessage
	edge.messageHandlers[spec.TypePing] = edge.handlePingMessage
	edge.messageHandlers[spec.TypeP2PFullState] = edge.handleP2PFullStateMessage
	edge.messageHandlers[spec.TypeLeasesInfos] = edge.handleLeasesInfosMessage
	edge.messageHandlers[spec.TypeRetryRegisterRequest] = edge.handleRetryRegisterRequest
	edge.messageHandlers[spec.TypeRegisterResponse] = edge.handleRegisterResponseMessage
	edge.messageHandlers[spec.TypeSNPublicSecret] = edge.handleSNPublicSecretMessage
	return edge, nil
}

// Run launches heartbeat, TAP-to-supernode, and UDP-to-TAP goroutines.
func (e *EdgeClient) Run() {
	if !e.running.CompareAndSwap(false, true) {
		log.Printf("Edge: Already running, ignoring Run() call")
		return
	}
	if !e.registered {
		log.Printf("Edge: Cannot run an unregistered edge, ignoring Run() call")
	}

	go e.handleHeartbeat()
	go e.handleTAP()
	go e.handleUDP()

	log.Printf("Edge: sending preliminary Peer List Request")
	err := e.sendPeerListRequest()
	if err != nil {
		log.Printf("Edge: (warn) failed sending preliminary Peer List Request: %v", err)
	}

	log.Printf("Edge: starting P2PUpdate routines...")
	go e.handleP2PUpdates()
	go e.handleP2PInfos()

	log.Printf("Edge: starting management api...")
	eapi := NewEdgeApi(e)
	e.EAPI = eapi
	go eapi.Run()

	<-e.ctx.Done() // Block until context is cancelled
}

// Close initiates a clean shutdown.
func (e *EdgeClient) Close() {
	if err := e.Unregister(); err != nil {
		log.Printf("Edge: Unregister failed: %v", err)
	}
	if e.IgdClient != nil {
		log.Printf(" UPnP > Cleaning up all portMappings...")
		e.IgdClient.CleanupAllMappings()
	}
	e.cancel()

	// Force read operations to unblock
	if e.Conn != nil {
		e.Conn.SetReadDeadline(time.Now())
	}

	// Wait for all goroutines to finish
	e.wg.Wait()

	// Close resources
	if e.TAP != nil {
		if err := e.TAP.Close(); err != nil {
			log.Printf("Edge: Error closing TAP interface: %v", err)
		}
	}

	if e.Conn != nil {
		if err := e.Conn.Close(); err != nil {
			log.Printf("Edge: Error closing UDP connection: %v", err)
		}
	}

	e.running.Store(false)
	log.Printf("Edge: Shutdown complete")
}

func (e *EdgeClient) IsSupernodeUDPAddr(addr *net.UDPAddr) bool {
	return (addr.IP.Equal(e.SupernodeAddr.IP)) && (addr.Port == e.SupernodeAddr.Port)
}

// ProtocolVersion returns the protocol version being used
func (e *EdgeClient) ProtocolVersion() uint8 {
	return protocol.VersionV
}

func (e *EdgeClient) EncryptedMachineID() ([]byte, error) {
	encMachineID, err := crypto.EncryptSequence(e.machineId, e.SNPubKey)
	if err != nil {
		return nil, err
	}
	return encMachineID, nil
}

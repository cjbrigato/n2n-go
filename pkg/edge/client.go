package edge

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/log"
	"n2n-go/pkg/machine"
	"n2n-go/pkg/management"
	"n2n-go/pkg/natclient"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/spec"
	transform "n2n-go/pkg/tranform"
	"n2n-go/pkg/tuntap"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
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

	//EncryptionKey     []byte
	//encryptionEnabled bool

	protocolVersion   uint8
	heartbeatInterval time.Duration
	verifyHash        bool
	enableVFuze       bool
	communityHash     uint32

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	NatClient natclient.NATClient

	VirtualIP       string
	ParsedVirtualIP net.IP
	MACAddr         net.HardwareAddr

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
	Mgmt *management.ManagementServer

	SNPubKey *rsa.PublicKey

	//state
	registered bool
	config     *Config
	// Handlers
	messageHandlers protocol.MessageHandlerMap

	isWaitingForSNPubKeyUpdate          bool
	isWaitingForSNRetryRegisterResponse bool

	payloadProcessor *transform.PayloadProcessor
}

func EnsureEdgeLogger() {
	log.MustInit("edge")
}

func NewEdgeManagementClient() (*management.ManagementClient, error) {
	cfg, err := LoadConfig(false) // Load config using Viper
	if err != nil {
		return nil, fmt.Errorf("Failed to load configuration: %w", err)
	}
	client := management.NewManagementClient("edge", cfg.Community)
	if isStarted := client.IsManagementServerStarted(); !isStarted {
		return nil, fmt.Errorf("Unable to connect to edge management (is edge up and running ?)")
	}
	return client, nil
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(cfg Config) (*EdgeClient, error) {

	machineId, err := machine.GetMachineID()
	if err != nil {
		return nil, err
	}
	log.Printf("got machine-id: %s", hex.EncodeToString(machineId))
	predictableMac, err := machine.GenerateMac(cfg.Community)
	if err != nil {
		return nil, err
	}
	log.Printf("%s TAP ifName machine-id based Community %s MAC Address: %s", cfg.TapName, cfg.Community, predictableMac.String())

	tapcfg := tuntap.Config{
		Name:       cfg.TapName,
		DevType:    tuntap.TAP,
		MACAddress: predictableMac.String(), // Windows: Set via registry
	}

	conn, tap, snAddr, err := setupNetworkComponents(cfg, tapcfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	communityHash := protocol.HashCommunity(cfg.Community)

	natClient := natclient.SetupNAT(conn, cfg.EdgeID, cfg.SupernodeAddr)

	err = tap.IfMac(predictableMac.String())
	if err != nil {
		if runtime.GOOS == "windows" {
			log.Printf("warn: %v", err)
		} else {
			log.Fatalf("err: %v", err)
		}
	}

	processorTransforms := []transform.Transform{}
	if cfg.CompressPayload {
		zstdTransform, err := transform.NewZstdTransform(zstd.SpeedDefault)
		if err != nil {
			log.Fatalf(" unable to create zstdTransform for payloadProcessor (requested by compress-payload): %v", err)
		}
		log.Printf("added zstdTransform to payload Processor (compress-payload is true)")
		log.Printf("ensure all connected edges also uses compress-payload settings !")
		processorTransforms = append(processorTransforms, zstdTransform)
	}
	if cfg.EncryptionPassphrase != "" {
		aesGCMTransform, err := transform.NewAESGCMTransform(cfg.EncryptionPassphrase)
		if err != nil {
			log.Fatalf("cannot instanciate aesGCMTransform for payloadProcessor: %v", err)
		}
		log.Printf("added AESGCMTransform to payload Processor (encryption-passphrase is set)")
		log.Printf("Attention! Encryption of data packets payload is enabled. Ensure all edge for the community uses same passphrase !")
		processorTransforms = append(processorTransforms, aesGCMTransform)
	}

	if len(processorTransforms) < 1 {
		log.Printf("added NoOpTransform to payload Processor since none have been enabled")
		processorTransforms = append(processorTransforms, transform.NewNoOpTransform())
	}

	payloadProcessor, err := transform.NewPayloadProcessor(processorTransforms)
	if err != nil {
		log.Fatalf("cannot instanciate payloadProcessor: %v", err)
	}

	mgmtServer := management.NewManagementServer("edge", cfg.Community)
	err = mgmtServer.Start()
	if err != nil {
		log.Fatalf("Failed to start management server: %v", err)
	}

	edge := &EdgeClient{
		Peers:             p2p.NewPeerRegistry(cfg.Community),
		ID:                cfg.EdgeID,
		Community:         cfg.Community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		Mgmt:              mgmtServer,
		seq:               0,
		NatClient:         natClient,
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
		payloadProcessor:  payloadProcessor,
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
		log.Printf("Already running, ignoring Run() call")
		return
	}
	if !e.registered {
		log.Printf("Cannot run an unregistered edge, ignoring Run() call")
	}

	go e.handleHeartbeat()
	go e.handleTAP()
	go e.handleUDP()

	log.Printf("sending preliminary Peer List Request")
	err := e.sendPeerListRequest()
	if err != nil {
		log.Printf("(warn) failed sending preliminary Peer List Request: %v", err)
	}

	log.Printf("starting P2PUpdate routines...")
	go e.handleP2PUpdates()
	go e.handleP2PInfos()

	log.Printf("starting management api...")
	eapi := NewEdgeApi(e)
	e.EAPI = eapi
	go eapi.Run()

	<-e.ctx.Done() // Block until context is cancelled
}

// Close initiates a clean shutdown.
func (e *EdgeClient) Close() {
	if err := e.Unregister(); err != nil {
		log.Printf("Unregister failed: %v", err)
	}
	if e.NatClient != nil {
		log.Printf("nat: Cleaning up all portMappings...")
		natclient.Cleanup(e.NatClient)
	}
	e.Mgmt.Stop()
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
			log.Printf("Error closing TAP interface: %v", err)
		}
	}

	if e.Conn != nil {
		if err := e.Conn.Close(); err != nil {
			log.Printf("Error closing UDP connection: %v", err)
		}
	}

	e.running.Store(false)
	log.Printf("Shutdown complete")
}

func (e *EdgeClient) IsKnownPeerSocket(addr *net.UDPAddr) bool {
	peer, err := e.Peers.GetPeerBySocket(addr)
	if err != nil || peer == nil {
		return false
	}
	return true
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

func (e *EdgeClient) ProcessOutgoingPayload(payload []byte) ([]byte, error) {
	return e.payloadProcessor.PrepareOutput(payload)
}

func (e *EdgeClient) ProcessIncomingPayload(payload []byte) ([]byte, error) {
	return e.payloadProcessor.ParseInput(payload)
}

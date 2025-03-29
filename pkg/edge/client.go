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

package edge

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/machine"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
	"n2n-go/pkg/upnp"
	"n2n-go/pkg/util"
	"net"
	"strconv"
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

	//state
	registered bool
	config     *Config
	// Handlers
	messageHandlers protocol.MessageHandlerMap
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(cfg Config) (*EdgeClient, error) {

	/*if err := cfg.Defaults(); err != nil {
		return nil, err
	}*/

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

	edge := &EdgeClient{
		Peers:             p2p.NewPeerRegistry(),
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
	}
	edge.messageHandlers[protocol.TypeData] = edge.handleDataMessage
	edge.messageHandlers[protocol.TypePeerInfo] = edge.handlePeerInfoMessage
	edge.messageHandlers[protocol.TypePing] = edge.handlePingMessage
	edge.messageHandlers[protocol.TypeP2PFullState] = edge.handleP2PFullStateMessage
	edge.messageHandlers[protocol.TypeLeasesInfos] = edge.handleLeasesInfosMessage
	return edge, nil
}

// setupNetworkComponents initializes the UDP connection and TAP interface
func setupNetworkComponents(cfg Config) (*net.UDPConn, *tuntap.Interface, *net.UDPAddr, error) {
	snAddr, err := net.ResolveUDPAddr("udp4", cfg.SupernodeAddr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("edge: failed to resolve supernode address: %w", err)
	}

	conn, err := setupUDPConnection(cfg.LocalPort, cfg.UDPBufferSize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("edge: %w", err)
	}

	tap, err := tuntap.NewInterface(cfg.TapName, "tap")
	if err != nil {
		conn.Close() // Clean up on error
		return nil, nil, nil, fmt.Errorf("edge: failed to create TAP interface: %w", err)
	}

	return conn, tap, snAddr, nil
}

// setupUDPConnection creates and configures a UDP connection with the specified parameters
func setupUDPConnection(localPort int, bufferSize int) (*net.UDPConn, error) {
	localAddr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(localPort))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve local UDP address: %w", err)
	}

	// Set larger buffer sizes for UDP
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to open UDP connection: %w", err)
	}

	// Set UDP buffer sizes to reduce latency
	if err := conn.SetReadBuffer(bufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP read buffer size: %v", err)
	}
	if err := conn.SetWriteBuffer(bufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP write buffer size: %v", err)
	}

	return conn, nil
}

func SetupUPnP(conn *net.UDPConn, edgeID string) *upnp.UPnPClient {
	udpPort := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	log.Printf("Edge: seeking for an optional UPnP/IGD<1|2> support to ease with nat traversal...")
	igdClient, err := upnp.NewUPnPClient()
	if err != nil {
		log.Printf("Edge: unable to use UPnP/IGD on this network: %v", err)
	} else {
		description := fmt.Sprintf("n2n-go.portmap for %s client", edgeID)
		leaseDuration := uint32(0)
		protocol := "udp"
		log.Printf("Edge: Discovered IGD on network ! Starting upnpClient thread...")
		log.Printf(" UPnP > Successfully connected to IGD (%s)", igdClient.GatewayType)
		log.Printf(" UPnP > Local IP: %s", igdClient.LocalIP)
		log.Printf(" UPnP > External IP: %s", igdClient.ExternalIP)
		log.Printf(" UPnP > Creating port mapping: %s %d -> %s:%d (%s)",
			"udp", udpPort, igdClient.LocalIP, udpPort, edgeID)
		err = igdClient.AddPortMapping(
			protocol,
			udpPort,
			udpPort,
			description,
			leaseDuration,
		)
		if err != nil {
			log.Printf("UPnP: Failed to add port mapping: %v :-(", err)
			igdClient = nil
		} else {
			log.Println(" UPnP > Port mapping added successfully (will be automatically deleted when edge closes)")
		}
	}
	return igdClient
}

func (e *EdgeClient) Setup() error {
	if err := e.Register(); err != nil {
		return err
	}

	if err := e.TunUp(); err != nil {
		return err
	}

	log.Printf("Edge: sending preliminary gratuitous ARP")
	if err := e.sendGratuitousARP(); err != nil {
		return err
	}

	return nil
}

func (e *EdgeClient) sendGratuitousARP() error {
	return util.SendGratuitousARP(e.TAP.Name(), e.TAP.HardwareAddr(), net.ParseIP(e.VirtualIP))
}

func (e *EdgeClient) TunUp() error {
	if e.VirtualIP == "" {
		return fmt.Errorf("cannot configure TAP link before VirtualIP is set")
	}
	//return e.TAP.IfUp(e.VirtualIP)
	return util.IfUp(e.TAP.Name(), e.VirtualIP)
}

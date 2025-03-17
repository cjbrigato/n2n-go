package edge

import (
	"context"
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
	"n2n-go/pkg/util"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds the edge node configuration
type Config struct {
	UDPBufferSize     int
	EdgeID            string
	Community         string
	TapName           string
	LocalPort         int
	SupernodeAddr     string
	HeartbeatInterval time.Duration
	ProtocolVersion   uint8 // Protocol version to use
	VerifyHash        bool  // Whether to verify community hash
	EnableVFuze       bool  // if enabled, experimental fastpath when known peer
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval: 30 * time.Second,
		ProtocolVersion:   protocol.VersionV,
		VerifyHash:        true, // Verify community hash by default
		EnableVFuze:       true,
		UDPBufferSize:     8192 * 8192,
	}
}

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	Peers *PeerRegistry

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

	VirtualIP string
	MACAddr   net.HardwareAddr

	unregisterOnce sync.Once
	running        atomic.Bool

	// Buffer pools
	packetBufPool *buffers.BufferPool
	headerBufPool *buffers.BufferPool

	// Stats
	PacketsSent atomic.Uint64
	PacketsRecv atomic.Uint64

	//state
	registered bool

	// Handlers
	messageHandlers protocol.MessageHandlerMap
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(cfg Config) (*EdgeClient, error) {
	if cfg.EdgeID == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		cfg.EdgeID = h
	}

	// Set default values if not provided
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}

	snAddr, err := net.ResolveUDPAddr("udp4", cfg.SupernodeAddr)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve supernode address: %w", err)
	}

	localAddr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(cfg.LocalPort))
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve local UDP address: %w", err)
	}

	// Set larger buffer sizes for UDP
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to open UDP connection: %w", err)
	}

	// Set UDP buffer sizes to reduce latency
	if err := conn.SetReadBuffer(cfg.UDPBufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP read buffer size: %v", err)
	}
	if err := conn.SetWriteBuffer(cfg.UDPBufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP write buffer size: %v", err)
	}

	tap, err := tuntap.NewInterface(cfg.TapName, "tap")
	if err != nil {
		conn.Close() // Clean up on error
		return nil, fmt.Errorf("edge: failed to create TAP interface: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Calculate community hash
	communityHash := protocol.HashCommunity(cfg.Community)
	log.Printf("(workaround udev tap delay changing TUNTAP MacADDR) Sleeping 3sec")
	time.Sleep(3 * time.Second)
	edge := &EdgeClient{
		Peers:             NewPeerRegistry(),
		ID:                cfg.EdgeID,
		Community:         cfg.Community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		seq:               0,
		MACAddr:           tap.HardwareAddr(),
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
	}
	edge.messageHandlers[protocol.TypeData] = edge.handleDataMessage
	edge.messageHandlers[protocol.TypePeerInfo] = edge.handlePeerInfoMessage
	edge.messageHandlers[protocol.TypePing] = edge.handlePingMessage
	return edge, nil
}

// Register sends a registration packet to the supernode.
// Registration payload format depends on the header type:
// - Legacy: "REGISTER <edgeID> <tapMAC>" (MAC in hex colon-separated form)
// - Compact: "REGISTER <edgeID> <tapMAC> <community> <communityHash>" when extended addressing is used
// - ProtoV: "REGISTER <edgeDesc> <CommunityName>"
func (e *EdgeClient) Register() error {
	log.Printf("Registering with supernode at %s...", e.SupernodeAddr)

	payloadStr := fmt.Sprintf("REGISTER %s %s ",
		e.ID, e.Community)
	err := e.WritePacket(protocol.TypeRegister, e.MACAddr, payloadStr, UDPEnforceSupernode)
	if err != nil {
		return err
	}

	// Set a timeout for the response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Read the response
	respBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(respBuf)

	n, addr, err := e.Conn.ReadFromUDP(respBuf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %w", err)
	}

	// Reset deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	// Process the response
	resp := strings.TrimSpace(string(respBuf[:n]))
	parts := strings.Fields(resp)
	if len(parts) < 1 || parts[0] != "ACK" {
		if strings.HasPrefix(resp, "ERR") {
			return fmt.Errorf("edge: registration error: %s", resp)
		}
		return fmt.Errorf("edge: unexpected registration response from %v: %s", addr, resp)
	}

	if len(parts) >= 3 {
		e.VirtualIP = fmt.Sprintf("%s/%s", parts[1], parts[2])
		log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP)
	} else {
		return fmt.Errorf("edge: registration response missing virtual IP")
	}

	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	e.registered = true
	return nil
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
	return SendGratuitousARP(e.TAP.Name(), e.TAP.HardwareAddr(), net.ParseIP(e.VirtualIP))
}

// Unregister sends an unregister packet to the supernode.
func (e *EdgeClient) Unregister() error {
	if !e.registered {
		return fmt.Errorf("cannot unregister an unregistered edge")
	}
	var unregErr error
	e.unregisterOnce.Do(func() {
		payloadStr := fmt.Sprintf("UNREGISTER %s ", e.ID)
		err := e.WritePacket(protocol.TypeUnregister, nil, payloadStr, UDPEnforceSupernode)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

func (e *EdgeClient) UDPAddrWithStrategy(dst net.HardwareAddr, strategy UDPWriteStrategy) (*net.UDPAddr, error) {
	var udpSocket *net.UDPAddr
	switch strategy {
	case UDPEnforceSupernode:
		udpSocket = e.SupernodeAddr
	case UDPBestEffort, UDPEnforceP2P:
		if dst == nil {
			if strategy == UDPEnforceP2P {
				return nil, fmt.Errorf("cannot write packet with UDPEnforceP2P flag and a nil destMACAddr")
			}
			udpSocket = e.SupernodeAddr
			break
		}
		peer, err := e.Peers.GetPeer(dst.String())
		if err != nil {
			if strategy == UDPEnforceP2P {
				return nil, fmt.Errorf("cannot write packet with UDPEnforceP2P (peer not found) %v", err)
			}
			udpSocket = e.SupernodeAddr
			break
		}
		if peer.P2PStatus != P2PAvailable && strategy == UDPBestEffort {
			udpSocket = e.SupernodeAddr
			break
		}
		udpSocket = peer.UDPAddr()
	}
	return udpSocket, nil
}

func (e *EdgeClient) WritePacket(pt protocol.PacketType, dst net.HardwareAddr, payloadStr string, strategy UDPWriteStrategy) error {

	udpSocket, err := e.UDPAddrWithStrategy(dst, strategy)
	if err != nil {
		return err
	}

	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		e.ProtocolVersion(),
		64,
		pt,
		seq,
		e.Community,
		e.MACAddr,
		dst,
	)
	if err != nil {
		return err
	}

	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to protov %s header: %w", pt.String(), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], []byte(payloadStr))
	totalLen = protocol.ProtoVHeaderSize + payloadLen
	e.PacketsSent.Add(1)

	// Send the packet
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)
	if err != nil {
		return fmt.Errorf("edge: failed to send packet: %w", err)
	}
	return nil
}

// sendPeerRequest sends a PeerRequest for all but sender's peerinfos
// scoped by community
func (e *EdgeClient) sendPeerRequest() error {
	//seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	err := e.WritePacket(protocol.TypePeerRequest, nil, fmt.Sprintf("PEERREQUEST %s ", e.Community), UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send peerRequest: %w", err)
	}
	return nil
}

// sendHeartbeat sends a single heartbeat message
func (e *EdgeClient) sendHeartbeat() error {
	//seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	err := e.WritePacket(protocol.TypeHeartbeat, nil, fmt.Sprintf("HEARTBEAT %s ", e.Community), UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send heartbeat: %w", err)
	}
	return nil
}

// handleHeartbeat sends heartbeat messages periodically
func (e *EdgeClient) handleHeartbeat() {
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := e.sendHeartbeat(); err != nil {
				log.Printf("Edge: Heartbeat error: %v", err)
			}
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *EdgeClient) handleTAPVFuze(destMAC net.HardwareAddr, n int, payloadBuf []byte, udpSocket *net.UDPAddr) error {
	vfuzh := protocol.VFuzeHeaderBytes(destMAC)
	totalLen := protocol.ProtoVFuzeSize + n
	packet := make([]byte, totalLen)
	copy(packet[0:7], vfuzh[0:7])
	copy(packet[7:], payloadBuf[:n])
	e.PacketsSent.Add(1)
	_, err := e.Conn.WriteToUDP(packet[:totalLen], udpSocket)
	if err != nil {
		return err
	}
	return nil
}

// handleTAP reads packets from the TAP interface and (potentially) sends them to the supernode.
func (e *EdgeClient) handleTAP() {
	e.wg.Add(1)
	defer e.wg.Done()

	// Preallocate the buffer once - no need to reallocate for each packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	// Create separate areas for header and payload
	headerSize := protocol.ProtoVHeaderSize

	headerBuf := packetBuf[:headerSize]
	payloadBuf := packetBuf[headerSize:]

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue processing
		}

		// Read directly into payload area to avoid a copy
		n, err := e.TAP.Read(payloadBuf)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP read error: %v", err)
			continue
		}

		if n < 14 {
			log.Printf("Edge: Packet too short to contain Ethernet header (%d bytes)", n)
			continue
		}

		ethertype, err := tuntap.GetEthertype(payloadBuf)
		if err != nil {
			log.Printf("Edge: Cannot parse link layer frame for Ethertype, skipping: %v", err)
			continue
		}
		if ethertype == tuntap.IPv6 {
			//log.Printf("Edge: (warn) skipping TAP frame with IPv6 Ethertype: %v", ethertype)
			continue
		}
		// Extract destination MAC address from Ethernet header (first 6 bytes)
		udpSocket := e.SupernodeAddr
		destMAC := tuntap.FastDestination(payloadBuf)
		if !tuntap.IsBroadcast(destMAC) {
			udpSocket, err = e.UDPAddrWithStrategy(destMAC, UDPBestEffort)
			if err != nil {
				log.Printf("Edge: Error getting udpSocket with Strategy in handleTAP with destMAC: %v", err)
				continue
			}
			if e.enableVFuze {
				err = e.handleTAPVFuze(destMAC, n, payloadBuf, udpSocket)
				if err != nil {
					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					log.Printf("Edge: Error sending packet with enableVFuze from TAP: %v with socket: %s", err, udpSocket)
				}
				continue
			}
		}

		// Create and marshal header
		seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

		// Create compact header with destination MAC
		header, err := protocol.NewProtoVHeader(
			e.ProtocolVersion(),
			64,
			protocol.TypeData,
			seq,
			e.Community,
			e.MACAddr,
			destMAC,
		)

		if err := header.MarshalBinaryTo(headerBuf); err != nil {
			log.Printf("Edge: Failed to marshal protov data header: %v", err)
			continue
		}

		// Update stats
		e.PacketsSent.Add(1)

		// Send packet (header is already at the beginning of packetBuf)
		totalLen := headerSize + n
		_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

func (e *EdgeClient) IsSupernodeUDPAddr(addr *net.UDPAddr) bool {
	return (addr.IP.Equal(e.SupernodeAddr.IP)) && (addr.Port == e.SupernodeAddr.Port)
}

// handleUDP reads packets from the UDP connection and writes the payload to the TAP interface.
func (e *EdgeClient) handleUDP() {
	e.wg.Add(1)
	defer e.wg.Done()

	// Preallocate buffer for receiving packets
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue processing
		}
		n, addr, err := e.Conn.ReadFromUDP(packetBuf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("Edge: UDP read error: %v", err)
			continue
		}

		if e.enableVFuze {
			if packetBuf[0] == protocol.VersionVFuze {
				payload := packetBuf[protocol.ProtoVFuzeSize:n]
				_, err = e.TAP.Write(payload)
				if err != nil {
					if strings.Contains(err.Error(), "file already closed") {
						return
					}
					log.Printf("Edge: TAP write error: %v", err)
				}
				continue
			}
		}

		// Handle short packets and ACKs
		if n < protocol.ProtoVHeaderSize { // Even compact headers have minimum size
			msg := strings.TrimSpace(string(packetBuf[:n]))
			if msg == "ACK" {
				// Just an ACK - nothing to do
				continue
			}
			log.Printf("Edge: Received packet too short from %v: %q", addr, msg)
			continue
		}

		e.PacketsRecv.Add(1)

		rawMsg, err := protocol.NewRawMessage(packetBuf, addr)
		if err != nil {
			log.Printf("Edge: error while parsing UDP Packet: %v", err)
			continue
		}

		handler, exists := e.messageHandlers[rawMsg.Header.PacketType]
		if !exists {
			log.Printf("Edge: Unknown packet type %d from %v", rawMsg.Header.PacketType, rawMsg.Addr)
			continue
		}
		err = handler(rawMsg)
		if err != nil {
			log.Printf("Edge: Error from messageHandler: %v", err)
		}
	}
}

func (e *EdgeClient) handleDataMessage(r *protocol.RawMessage) error {
	_, err := e.TAP.Write(r.Payload)
	if err != nil {
		if !strings.Contains(err.Error(), "file already closed") {
			log.Printf("Edge: TAP write error: %v", err)
			return err
		}
	}
	return nil
}

func (e *EdgeClient) handlePeerInfoMessage(r *protocol.RawMessage) error {
	peerMsg, err := r.ToPeerInfoMessage()
	if err != nil {
		return err
	}
	peerInfos := peerMsg.PeerInfoList
	err = e.Peers.HandlePeerInfoList(peerInfos, false, true)
	if err != nil {
		log.Printf("Edge: error in HandlePeerInfoList: %v", err)
		return err
	}
	return nil
}

func (e *EdgeClient) handlePingMessage(r *protocol.RawMessage) error {
	pingMsg, err := r.ToPingMessage()
	if err != nil {
		return err
	}
	if !pingMsg.IsPong {
		// swap dst/src
		dst, err := net.ParseMAC(pingMsg.EdgeMACAddr)
		if err != nil {
			return fmt.Errorf("cannot parse dst EdgeMACAddr for swaping")
		}
		if pingMsg.DestMACAddr != e.MACAddr.String() {
			return fmt.Errorf("ping recipient differs from this edge MACAddress")
		}
		payloadStr := fmt.Sprintf("PONG %s ", pingMsg.CheckID)
		e.WritePacket(protocol.TypePing, dst, payloadStr, UDPBestEffort)
	} else {
		peer, err := e.Peers.GetPeer(pingMsg.EdgeMACAddr)
		if err != nil {
			return fmt.Errorf("received a pong for a MACAddress %s not in our peers list", pingMsg.EdgeMACAddr)
		}
		if peer.P2PCheckID == pingMsg.CheckID {
			peer.UpdateP2PStatus(P2PAvailable, pingMsg.CheckID)
		} else {
			err = fmt.Errorf("received a pong for MACAddress %s but checkID differs (want %s, received %s)", pingMsg.EdgeMACAddr, peer.P2PCheckID, pingMsg.CheckID)
			peer.UpdateP2PStatus(P2PUnknown, "")
		}
	}
	return nil
}

func (e *EdgeClient) UpdatePeersP2PStates() {
	peers := e.Peers.GetP2PUnknownPeers()
	for _, p := range peers {
		err := e.PingPeer(p, 5, 1*time.Second, P2PPending)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
	peers = e.Peers.GetP2PendingPeers()
	for _, p := range peers {
		err := e.PingPeer(p, 5, 1*time.Second, P2PPending)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
	peers = e.Peers.GetP2PAvailablePeers()
	for _, p := range peers {
		err := e.PingPeer(p, 2, 300*time.Millisecond, P2PAvailable)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
}

func (e *EdgeClient) PingPeer(p *Peer, n int, interval time.Duration, status P2PCapacity) error {
	checkid := fmt.Sprintf("%s.%s.%s.%s.%d", e.ID, e.MACAddr.String(), p.Infos.MACAddr.String(), p.Infos.PubSocket.IP.String(), p.Infos.PubSocket.Port)
	payloadStr := fmt.Sprintf("PING %s ", checkid)
	p.UpdateP2PStatus(status, checkid)
	for range n {
		e.WritePacket(protocol.TypePing, p.Infos.MACAddr, payloadStr, UDPEnforceP2P)
	}
	return e.WritePacket(protocol.TypePing, p.Infos.MACAddr, payloadStr, UDPEnforceP2P)
}

// handleHeartbeat sends heartbeat messages periodically
func (e *EdgeClient) handleP2PUpdates() {
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.UpdatePeersP2PStates()
		case <-e.ctx.Done():
			return
		}
	}
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

	log.Printf("Edge: sending preliminary Peer Request")
	err := e.sendPeerRequest()
	if err != nil {
		log.Printf("Edge: (warn) failed sending preliminary Peer Request: %v", err)
	}

	log.Printf("Edge: starting P2PUpdate routine...")
	go e.handleP2PUpdates()

	<-e.ctx.Done() // Block until context is cancelled
}

// Close initiates a clean shutdown.
func (e *EdgeClient) Close() {
	if err := e.Unregister(); err != nil {
		log.Printf("Edge: Unregister failed: %v", err)
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

func (e *EdgeClient) TunUp() error {
	if e.VirtualIP == "" {
		return fmt.Errorf("cannot configure TAP link before VirtualIP is set")
	}
	//return e.TAP.IfUp(e.VirtualIP)
	return util.IfUp(e.TAP.Name(), e.VirtualIP)
}

// isBroadcastMAC returns true if the provided MAC address (in bytes) is the broadcast address.
func isBroadcastMAC(mac []byte) bool {
	if len(mac) != 6 {
		return false
	}
	for _, b := range mac {
		if b != 0xFF {
			return false
		}
	}
	return true
}

// ProtocolVersion returns the protocol version being used
func (e *EdgeClient) ProtocolVersion() uint8 {
	return protocol.VersionV
}

// GetHeaderSize returns the current header size in bytes
func (e *EdgeClient) GetHeaderSize() int {
	return protocol.ProtoVHeaderSize
}

// GetStats returns packet statistics
type EdgeStats struct {
	TotalPacketsSent uint64
	TotalPacketsRecv uint64
	CommunityHash    uint32
}

// GetStats returns current packet statistics
func (e *EdgeClient) GetStats() EdgeStats {
	PacketsSent := e.PacketsSent.Load()
	PacketsRecv := e.PacketsRecv.Load()

	return EdgeStats{
		TotalPacketsSent: PacketsSent,
		TotalPacketsRecv: PacketsRecv,
		CommunityHash:    e.communityHash,
	}
}

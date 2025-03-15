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
	EdgeID            string
	Community         string
	TapName           string
	LocalPort         int
	SupernodeAddr     string
	HeartbeatInterval time.Duration
	ProtocolVersion   uint8 // Protocol version to use
	VerifyHash        bool  // Whether to verify community hash
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval: 30 * time.Second,
		ProtocolVersion:   protocol.VersionV,
		VerifyHash:        true, // Verify community hash by default
	}
}

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	ID            string
	Community     string
	SupernodeAddr *net.UDPAddr
	Conn          *net.UDPConn
	TAP           *tuntap.Interface
	seq           uint32

	protocolVersion   uint8
	heartbeatInterval time.Duration
	verifyHash        bool
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
	if err := conn.SetReadBuffer(1024 * 1024); err != nil {
		log.Printf("Warning: couldn't increase UDP read buffer size: %v", err)
	}
	if err := conn.SetWriteBuffer(1024 * 1024); err != nil {
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

	return &EdgeClient{
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
		communityHash:     communityHash,
		ctx:               ctx,
		cancel:            cancel,
		packetBufPool:     buffers.PacketBufferPool,
		headerBufPool:     buffers.HeaderBufferPool,
	}, nil
}

// Register sends a registration packet to the supernode.
// Registration payload format depends on the header type:
// - Legacy: "REGISTER <edgeID> <tapMAC>" (MAC in hex colon-separated form)
// - Compact: "REGISTER <edgeID> <tapMAC> <community> <communityHash>" when extended addressing is used
// - ProtoV: "REGISTER <edgeDesc> <CommunityName>"
func (e *EdgeClient) Register() error {
	log.Printf("Registering with supernode at %s...", e.SupernodeAddr)

	payloadStr := fmt.Sprintf("REGISTER %s %s",
		e.ID, e.Community)
	err := e.WritePacket(protocol.TypeRegister, e.MACAddr, payloadStr)
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

	return nil
}

func (e *EdgeClient) Setup() error {
	if err := e.Register(); err != nil {
		return err
	}

	if err := e.TunUp(); err != nil {
		return err
	}

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
	var unregErr error
	e.unregisterOnce.Do(func() {
		payloadStr := fmt.Sprintf("UNREGISTER %s", e.ID)
		err := e.WritePacket(protocol.TypeUnregister, nil, payloadStr)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

func (e *EdgeClient) WritePacket(pt protocol.PacketType, dst net.HardwareAddr, payloadStr string) error {
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
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %w", err)
	}
	return nil
}

// sendHeartbeat sends a single heartbeat message
func (e *EdgeClient) sendHeartbeat() error {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	var totalLen int

	// Create compact header
	header, err := protocol.NewProtoVHeader(
		e.ProtocolVersion(),
		64,
		protocol.TypeHeartbeat,
		seq,
		e.Community,
		e.TAP.HardwareAddr(),
		nil,
	)
	if err != nil {
		return err
	}

	// Marshal header directly into packet buffer
	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to marshal protov heartbeat header: %w", err)
	}

	// Add payload after header
	payloadStr := fmt.Sprintf("HEARTBEAT %s", e.Community)
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], []byte(payloadStr))
	totalLen = protocol.ProtoVHeaderSize + payloadLen

	// Update stats
	e.PacketsSent.Add(1)

	// Send the packet
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send heartbeat: %w", err)
	}

	return nil
}

// runHeartbeat sends heartbeat messages periodically
func (e *EdgeClient) runHeartbeat() {
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

// runTAPToSupernode reads packets from the TAP interface and sends them to the supernode.
func (e *EdgeClient) runTAPToSupernode() {
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

		var vfuzh []byte
		// Extract destination MAC address from Ethernet header (first 6 bytes)
		var destMAC net.HardwareAddr
		if !isBroadcastMAC(payloadBuf[:6]) {
			destMAC = net.HardwareAddr(payloadBuf[:6])
			vfuzh = protocol.VFuzeHeaderBytes(destMAC)
			totalLen := protocol.ProtoVFuzeSize + n
			packet := make([]byte, totalLen)
			copy(packet[0:7], vfuzh[0:7])
			copy(packet[7:], payloadBuf[:n])
			e.PacketsSent.Add(1)
			_, err = e.Conn.WriteToUDP(packet[:totalLen], e.SupernodeAddr)
			if err != nil {
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				log.Printf("Edge: Error sending packet to supernode: %v", err)
			}
			continue
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
		_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

// runUDPToTAP reads packets from the UDP connection and writes the payload to the TAP interface.
func (e *EdgeClient) runUDPToTAP() {
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

		// Check first byte for version to determine header type
		version := packetBuf[0]
		var payload []byte

		if version == protocol.VersionV {
			if n < protocol.ProtoVHeaderSize {
				log.Printf("Edge: Packet too short for protov header from %v", addr)
				continue
			}

			var header protocol.ProtoVHeader
			if err := header.UnmarshalBinary(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
				log.Printf("Edge: Failed to unmarshal protov header from %v: %v", addr, err)
				continue
			}

			// Verify timestamp
			if !header.VerifyTimestamp(time.Now(), 16*time.Second) {
				log.Printf("Edge: protov header timestamp verification failed from %v", addr)
				continue
			}

			// Verify community hash if enabled
			if e.verifyHash && header.CommunityID != e.communityHash {
				log.Printf("Edge: Community hash mismatch: expected %d, got %d from %v",
					e.communityHash, header.CommunityID, addr)
				continue
			}

			payload = packetBuf[protocol.ProtoVHeaderSize:n]

			// Update stats
			e.PacketsRecv.Add(1)

		} else {
			log.Printf("Edge: Unknown header version %d from %v", version, addr)
			continue
		}

		// Write payload to TAP
		_, err = e.TAP.Write(payload)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP write error: %v", err)
		}
	}
}

// Run launches heartbeat, TAP-to-supernode, and UDP-to-TAP goroutines.
func (e *EdgeClient) Run() {
	if !e.running.CompareAndSwap(false, true) {
		log.Printf("Edge: Already running, ignoring Run() call")
		return
	}

	go e.runHeartbeat()
	go e.runTAPToSupernode()
	go e.runUDPToTAP()

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

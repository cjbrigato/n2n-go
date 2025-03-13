package edge

import (
	"context"
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	ID            string
	Community     string
	SupernodeAddr *net.UDPAddr
	Conn          *net.UDPConn
	TAP           *tuntap.Interface
	seq           uint32

	heartbeatInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	VirtualIP string

	unregisterOnce sync.Once
	running        atomic.Bool

	// Buffer pools
	packetBufPool *buffers.BufferPool
	headerBufPool *buffers.BufferPool
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(id, community, tapName string, localPort int, supernode string, heartbeatInterval time.Duration) (*EdgeClient, error) {
	snAddr, err := net.ResolveUDPAddr("udp4", supernode)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve supernode address: %w", err)
	}

	localAddr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(localPort))
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

	tap, err := tuntap.NewInterface(tapName, "tap")
	if err != nil {
		conn.Close() // Clean up on error
		return nil, fmt.Errorf("edge: failed to create TAP interface: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &EdgeClient{
		ID:                id,
		Community:         community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		seq:               0,
		heartbeatInterval: heartbeatInterval,
		ctx:               ctx,
		cancel:            cancel,
		packetBufPool:     buffers.PacketBufferPool,
		headerBufPool:     buffers.HeaderBufferPool,
	}, nil
}

// Register sends a registration packet to the supernode.
// Registration payload format: "REGISTER <edgeID> <tapMAC>" (MAC in hex colon-separated form).
func (e *EdgeClient) Register() error {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	// Get MAC address
	mac := e.TAP.HardwareAddr()
	if mac == nil {
		return fmt.Errorf("edge: failed to get TAP MAC address")
	}

	// Create header
	header := protocol.NewHeader(3, 64, protocol.TypeRegister, seq, e.Community, e.ID, "")

	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	// Marshal header directly into packet buffer
	if err := header.MarshalBinaryTo(packetBuf[:protocol.TotalHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %w", err)
	}

	// Add payload after header
	payloadStr := fmt.Sprintf("REGISTER %s %s", e.ID, mac.String())
	payloadLen := copy(packetBuf[protocol.TotalHeaderSize:], []byte(payloadStr))

	// Send the packet
	totalLen := protocol.TotalHeaderSize + payloadLen
	_, err := e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %w", err)
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

// Unregister sends an unregister packet to the supernode.
func (e *EdgeClient) Unregister() error {
	var unregErr error
	e.unregisterOnce.Do(func() {
		seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

		// Create header
		header := protocol.NewHeader(3, 64, protocol.TypeUnregister, seq, e.Community, e.ID, "")

		// Get buffer for full packet
		packetBuf := e.packetBufPool.Get()
		defer e.packetBufPool.Put(packetBuf)

		// Marshal header directly into packet buffer
		if err := header.MarshalBinaryTo(packetBuf[:protocol.TotalHeaderSize]); err != nil {
			unregErr = fmt.Errorf("edge: failed to marshal unregister header: %w", err)
			return
		}

		// Add payload after header
		payloadStr := fmt.Sprintf("UNREGISTER %s", e.ID)
		payloadLen := copy(packetBuf[protocol.TotalHeaderSize:], []byte(payloadStr))

		// Send the packet
		totalLen := protocol.TotalHeaderSize + payloadLen
		_, err := e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}

		log.Printf("Edge: Unregister message sent")
	})

	return unregErr
}

// sendHeartbeat sends a single heartbeat message
func (e *EdgeClient) sendHeartbeat() error {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	// Create header
	header := protocol.NewHeader(3, 64, protocol.TypeHeartbeat, seq, e.Community, e.ID, "")

	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	// Marshal header directly into packet buffer
	if err := header.MarshalBinaryTo(packetBuf[:protocol.TotalHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to marshal heartbeat header: %w", err)
	}

	// Add payload after header
	payloadStr := fmt.Sprintf("HEARTBEAT %s", e.ID)
	payloadLen := copy(packetBuf[protocol.TotalHeaderSize:], []byte(payloadStr))

	// Send the packet
	totalLen := protocol.TotalHeaderSize + payloadLen
	_, err := e.Conn.WriteToUDP(packetBuf[:totalLen], e.SupernodeAddr)
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
	headerBuf := packetBuf[:protocol.TotalHeaderSize]
	payloadBuf := packetBuf[protocol.TotalHeaderSize:]

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

		// Extract destination MAC address from Ethernet header (first 6 bytes)
		var destMAC net.HardwareAddr
		if !isBroadcastMAC(payloadBuf[:6]) {
			destMAC = net.HardwareAddr(payloadBuf[:6])
		}

		// Create and marshal header
		seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
		header := protocol.NewHeaderWithDestMAC(3, 64, protocol.TypeData, seq, e.Community, e.ID, destMAC)

		if err := header.MarshalBinaryTo(headerBuf); err != nil {
			log.Printf("Edge: Failed to marshal data header: %v", err)
			continue
		}

		// Send packet (header is already at the beginning of packetBuf)
		totalLen := protocol.TotalHeaderSize + n
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

	var header protocol.Header

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

		// Handle short packets and ACKs
		if n < protocol.TotalHeaderSize {
			msg := strings.TrimSpace(string(packetBuf[:n]))
			if msg == "ACK" {
				// Just an ACK - nothing to do
				continue
			}
			log.Printf("Edge: Received packet too short from %v: %q", addr, msg)
			continue
		}

		// Unmarshal header
		if err := header.UnmarshalBinary(packetBuf[:protocol.TotalHeaderSize]); err != nil {
			log.Printf("Edge: Failed to unmarshal header from %v: %v", addr, err)
			continue
		}

		// Verify timestamp
		if !header.VerifyTimestamp(time.Now(), 16*time.Second) {
			log.Printf("Edge: Header timestamp verification failed from %v", addr)
			continue
		}

		// Extract payload and write to TAP
		payload := packetBuf[protocol.TotalHeaderSize:n]
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

package edge

import (
	"context"
	"fmt"
	"log"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BufferSize defines the maximum packet size to handle
const BufferSize = 2048

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

	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to open UDP connection: %w", err)
	}

	tap, err := tuntap.NewInterface(tapName, "tap")
	if err != nil {
		conn.Close() // Close the connection if TAP creation fails
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
	}, nil
}

// Register sends a registration packet to the supernode.
// Registration payload format: "REGISTER <edgeID> <tapMAC>" (MAC in hex colon-separated form).
func (e *EdgeClient) Register() error {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	// Use control header (dest remains empty).
	header := protocol.NewHeader(3, 64, protocol.TypeRegister, seq, e.Community, e.ID, "")
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %w", err)
	}

	mac := e.TAP.HardwareAddr()
	if mac == nil {
		return fmt.Errorf("edge: failed to get TAP MAC address")
	}

	payload := []byte(fmt.Sprintf("REGISTER %s %s", e.ID, mac.String()))
	packet := append(headerBytes, payload...)

	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %w", err)
	}

	// Set a timeout for registration response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Use a larger buffer to handle potential large responses
	buf := make([]byte, BufferSize)
	n, addr, err := e.Conn.ReadFromUDP(buf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %w", err)
	}

	// Reset the deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	resp := strings.TrimSpace(string(buf[:n]))
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
		header := protocol.NewHeader(3, 64, protocol.TypeUnregister, seq, e.Community, e.ID, "")
		headerBytes, err := header.MarshalBinary()
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to marshal unregister header: %w", err)
			return
		}

		payload := []byte(fmt.Sprintf("UNREGISTER %s", e.ID))
		packet := append(headerBytes, payload...)

		_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}

		log.Printf("Edge: Unregister message sent")
	})

	return unregErr
}

// sendHeartbeat sends a single heartbeat message to the supernode
func (e *EdgeClient) sendHeartbeat() error {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	header := protocol.NewHeader(3, 64, protocol.TypeHeartbeat, seq, e.Community, e.ID, "")
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal heartbeat header: %w", err)
	}

	payload := []byte(fmt.Sprintf("HEARTBEAT %s", e.ID))
	packet := append(headerBytes, payload...)

	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send heartbeat: %w", err)
	}

	return nil
}

// runHeartbeat sends heartbeat messages periodically.
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

// processAndSendPacket handles the packet processing and sending logic
func (e *EdgeClient) processAndSendPacket(packet []byte) error {
	destMACBytes := packet[0:6]
	var destMAC net.HardwareAddr

	if !isBroadcastMAC(destMACBytes) {
		destMAC = destMACBytes
	}

	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	header := protocol.NewHeaderWithDestMAC(3, 64, protocol.TypeData, seq, e.Community, e.ID, destMAC)

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal data header: %w", err)
	}

	fullPacket := append(headerBytes, packet...)

	_, err = e.Conn.WriteToUDP(fullPacket, e.SupernodeAddr)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return nil // Silently handle closed connection
		}
		return fmt.Errorf("error sending packet to supernode: %w", err)
	}

	return nil
}

// runTAPToSupernode reads packets from the TAP interface and sends them to the supernode.
func (e *EdgeClient) runTAPToSupernode() {
	e.wg.Add(1)
	defer e.wg.Done()

	buf := make([]byte, BufferSize)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue with normal operation
		}

		n, err := e.TAP.Read(buf)
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

		if err := e.processAndSendPacket(buf[:n]); err != nil {
			log.Printf("Edge: Failed to send packet: %v", err)
		}
	}
}

// processReceivedPacket handles packet processing logic for received UDP packets
func (e *EdgeClient) processReceivedPacket(packet []byte, addr *net.UDPAddr) error {
	if len(packet) < protocol.TotalHeaderSize {
		msg := strings.TrimSpace(string(packet))
		if msg == "ACK" {
			return nil // ACK messages are expected and ignored
		}
		return fmt.Errorf("received packet too short from %v: %q", addr, msg)
	}

	var hdr protocol.Header
	if err := hdr.UnmarshalBinary(packet[:protocol.TotalHeaderSize]); err != nil {
		return fmt.Errorf("failed to unmarshal header from %v: %w", addr, err)
	}

	if !hdr.VerifyTimestamp(time.Now(), 16*time.Second) {
		return fmt.Errorf("header timestamp verification failed from %v", addr)
	}

	payload := packet[protocol.TotalHeaderSize:]
	_, err := e.TAP.Write(payload)
	if err != nil {
		if strings.Contains(err.Error(), "file already closed") {
			return nil // Silently handle closed TAP interface
		}
		return fmt.Errorf("TAP write error: %w", err)
	}

	return nil
}

// runUDPToTAP reads packets from the UDP connection and writes the payload to the TAP interface.
func (e *EdgeClient) runUDPToTAP() {
	e.wg.Add(1)
	defer e.wg.Done()

	buf := make([]byte, BufferSize)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue with normal operation
		}

		n, addr, err := e.Conn.ReadFromUDP(buf)
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

		if err := e.processReceivedPacket(buf[:n], addr); err != nil {
			log.Printf("Edge: Failed to process packet: %v", err)
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

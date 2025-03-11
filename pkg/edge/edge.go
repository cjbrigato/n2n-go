// Package edge implements the client (edge) functionality,
// integrating protocol framing for registration, heartbeat, and data forwarding.
package edge

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
)

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	ID            string
	Community     string
	SupernodeAddr *net.UDPAddr
	Conn          *net.UDPConn
	TUN           *tuntap.Interface
	seq           uint16

	heartbeatInterval time.Duration
	quitHeartbeat     chan struct{}
}

// NewEdgeClient creates a new EdgeClient.
// It resolves the supernode address, opens a UDP socket, and sets up a TUN interface.
func NewEdgeClient(id, community, tunName string, localPort int, supernode string, heartbeatInterval time.Duration) (*EdgeClient, error) {
	snAddr, err := net.ResolveUDPAddr("udp", supernode)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve supernode address: %v", err)
	}
	localAddr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(localPort))
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve local UDP address: %v", err)
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to open UDP connection: %v", err)
	}
	tun, err := tuntap.NewInterface(tunName, "tun")
	if err != nil {
		return nil, fmt.Errorf("edge: failed to create TUN interface: %v", err)
	}
	return &EdgeClient{
		ID:                id,
		Community:         community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TUN:               tun,
		seq:               0,
		heartbeatInterval: heartbeatInterval,
		quitHeartbeat:     make(chan struct{}),
	}, nil
}

// Register sends a registration message to the supernode.
func (e *EdgeClient) Register() error {
	e.seq++
	header := protocol.NewPacketHeader(3, 64, 0, e.seq, e.Community)
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %v", err)
	}
	payload := []byte(fmt.Sprintf("REGISTER %s", e.ID))
	packet := append(headerBytes, payload...)
	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %v", err)
	}

	// Set a deadline for receiving the ACK.
	e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	n, addr, err := e.Conn.ReadFromUDP(buf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %v", err)
	}
	ack := strings.TrimSpace(string(buf[:n]))
	if ack != "ACK" {
		return fmt.Errorf("edge: unexpected registration response from %v: %s", addr, ack)
	}
	log.Printf("Edge: Registration successful (ACK from %v)", addr)

	// Clear the read deadline to avoid spurious timeouts during normal operation.
	e.Conn.SetReadDeadline(time.Time{})
	return nil
}

// startHeartbeat sends heartbeat messages periodically to refresh registration.
func (e *EdgeClient) startHeartbeat() {
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.sendHeartbeat()
		case <-e.quitHeartbeat:
			return
		}
	}
}

// sendHeartbeat constructs and sends a heartbeat packet.
func (e *EdgeClient) sendHeartbeat() {
	e.seq++
	header := protocol.NewPacketHeader(3, 64, 1, e.seq, e.Community) // Flag 1 indicates heartbeat.
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		log.Printf("Edge: Failed to marshal heartbeat header: %v", err)
		return
	}
	payload := []byte("HEARTBEAT")
	packet := append(headerBytes, payload...)
	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		log.Printf("Edge: Failed to send heartbeat: %v", err)
	}
}

// Run starts the edge client:
// - A heartbeat goroutine sending periodic heartbeats.
// - A goroutine to forward packets from the TUN interface to the supernode.
// - The main loop reads from UDP (from the supernode) and writes to the TUN interface.
func (e *EdgeClient) Run() {
	go e.startHeartbeat()

	// Goroutine: Forward TUN traffic to supernode.
	go func() {
		buf := make([]byte, 1500)
		for {
			n, err := e.TUN.Read(buf)
			if err != nil {
				log.Printf("Edge: TUN read error: %v", err)
				continue
			}
			e.seq++
			header := protocol.NewPacketHeader(3, 64, 0, e.seq, e.Community)
			headerBytes, err := header.MarshalBinary()
			if err != nil {
				log.Printf("Edge: Failed to marshal header: %v", err)
				continue
			}
			packet := append(headerBytes, buf[:n]...)
			_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
			if err != nil {
				log.Printf("Edge: Error sending packet to supernode: %v", err)
			}
		}
	}()

	// Main loop: Forward UDP traffic from supernode to TUN.
	buf := make([]byte, 1500)
	for {
		n, addr, err := e.Conn.ReadFromUDP(buf)
		if err != nil {
			// Check for timeout error and skip logging if so.
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("Edge: UDP read error: %v", err)
			continue
		}
		if n < protocol.TotalHeaderSize {
			log.Printf("Edge: Received packet too short from %v", addr)
			continue
		}
		var hdr protocol.PacketHeader
		if err := hdr.UnmarshalBinary(buf[:protocol.TotalHeaderSize]); err != nil {
			log.Printf("Edge: Failed to unmarshal header from %v: %v", addr, err)
			continue
		}
		if !hdr.VerifyTimestamp(time.Now(), 16*time.Second) {
			log.Printf("Edge: Header timestamp verification failed from %v", addr)
			continue
		}
		payload := buf[protocol.TotalHeaderSize:n]
		_, err = e.TUN.Write(payload)
		if err != nil {
			log.Printf("Edge: TUN write error: %v", err)
		}
	}
}

// Close stops the heartbeat and closes the TUN interface and UDP connection.
func (e *EdgeClient) Close() {
	close(e.quitHeartbeat)
	if e.TUN != nil {
		e.TUN.Close()
	}
	if e.Conn != nil {
		e.Conn.Close()
	}
}
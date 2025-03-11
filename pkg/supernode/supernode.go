// Package supernode maintains a registry of registered edges (peers)
// and processes incoming packets from edges using protocol framing.
// It now provides functions to register/update edges (RegisterEdge) and
// to cleanup stale entries (CleanupStaleEdges).
package supernode

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"n2n-go/pkg/protocol"
)

// Edge represents a registered edge.
type Edge struct {
	ID            string    // Unique edge identifier
	PublicIP      net.IP    // Edge's public IP address
	Port          int       // Edge's UDP port
	Community     string    // Community or edge identifier (from header)
	LastHeartbeat time.Time // Last time a packet was received from this edge
	LastSequence  uint16    // Last sequence number received
}

// Supernode holds the registry of edges and a UDP connection.
type Supernode struct {
	mu    sync.RWMutex
	edges map[string]*Edge
	Conn  *net.UDPConn
}

// NewSupernode creates a new Supernode instance using an established UDP connection.
func NewSupernode(conn *net.UDPConn) *Supernode {
	return &Supernode{
		edges: make(map[string]*Edge),
		Conn:  conn,
	}
}

// RegisterEdge adds a new edge record or updates an existing one.
// In this implementation, we assume the edge ID is stored in the Community field.
func (s *Supernode) RegisterEdge(id string, publicIP net.IP, port int, community string, seq uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, exists := s.edges[id]
	if !exists {
		edge = &Edge{
			ID:            id,
			PublicIP:      publicIP,
			Port:          port,
			Community:     community,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
		}
		s.edges[id] = edge
		log.Printf("Supernode: New edge registered: %s", id)
	} else {
		edge.PublicIP = publicIP
		edge.Port = port
		edge.Community = community
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		log.Printf("Supernode: Edge updated: %s", id)
	}
}

// CleanupStaleEdges removes edge records that haven't been updated within the expiry duration.
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			delete(s.edges, id)
			log.Printf("Supernode: Edge %s removed due to stale heartbeat", id)
		}
	}
}

// ProcessPacket parses an incoming packet using the protocol package, updates the edge registry,
// and processes the packet payload. It uses the Community field (trimmed) as the edge identifier.
func (s *Supernode) ProcessPacket(packet []byte, addr *net.UDPAddr) {
	if len(packet) < protocol.TotalHeaderSize {
		log.Printf("Supernode: Packet too short from %v", addr)
		return
	}
	var hdr protocol.PacketHeader
	if err := hdr.UnmarshalBinary(packet[:protocol.TotalHeaderSize]); err != nil {
		log.Printf("Supernode: Failed to unmarshal header from %v: %v", addr, err)
		return
	}

	// Use the Community field as the edge ID.
	edgeID := strings.TrimRight(string(hdr.Community[:]), "\x00")
	s.RegisterEdge(edgeID, addr.IP, addr.Port, edgeID, hdr.Sequence)

	payload := packet[protocol.TotalHeaderSize:]
	msg := strings.TrimSpace(string(payload))
	isHeartbeat := (hdr.Flags == 1) || (msg == "HEARTBEAT")
	if isHeartbeat {
		log.Printf("Supernode: Received heartbeat from edge %s", edgeID)
	} else {
		log.Printf("Supernode: Received data packet from edge %s: seq=%d, payloadLen=%d",
			edgeID, hdr.Sequence, len(payload))
		// Process additional payload if needed.
	}

	// Send an ACK response.
	if err := s.SendAck(addr); err != nil {
		log.Printf("Supernode: Failed to send ACK to %v: %v", addr, err)
	}
}

// SendAck sends an "ACK" message back to the specified address.
func (s *Supernode) SendAck(addr *net.UDPAddr) error {
	ack := []byte("ACK")
	_, err := s.Conn.WriteToUDP(ack, addr)
	return err
}

// Listen continuously receives UDP packets and processes them concurrently.
func (s *Supernode) Listen() {
	buf := make([]byte, 1600)
	for {
		n, addr, err := s.Conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("Supernode: UDP read error: %v", err)
			continue
		}
		go s.ProcessPacket(buf[:n], addr)
	}
}

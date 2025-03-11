// Package supernode maintains a registry of registered edges (peers)
// and processes incoming packets from edges using protocol framing.
// It provides functions to register/update edges and cleanup stale entries.
package supernode

import (
	"fmt"
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
	Community     string    // Community name (used here as edge identifier)
	VirtualIP     net.IP    // Assigned virtual IP address
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
// The edge's VirtualIP is calculated here based on its ID.
func (s *Supernode) RegisterEdge(id string, publicIP net.IP, port int, community string, seq uint16) *Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, exists := s.edges[id]
	if !exists {
		// For demonstration, we generate a virtual IP by appending the last octet from a hash.
		// In production, use a more robust allocation mechanism.
		virtualOctet := uint8(seq%254 + 1)
		vip := net.IPv4(10, 0, 0, virtualOctet)
		edge = &Edge{
			ID:            id,
			PublicIP:      publicIP,
			Port:          port,
			Community:     community,
			VirtualIP:     vip,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
		}
		s.edges[id] = edge
		log.Printf("Supernode: New edge registered: %s, assigned VIP: %s", id, vip.String())
	} else {
		edge.PublicIP = publicIP
		edge.Port = port
		edge.Community = community
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		log.Printf("Supernode: Edge updated: %s", id)
	}
	return edge
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

// ProcessPacket parses an incoming packet using the protocol package,
// updates the edge registry, and sends an appropriate ACK.
// For registration messages, it includes the assigned virtual IP in the ACK.
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

	payload := packet[protocol.TotalHeaderSize:]
	msg := strings.TrimSpace(string(payload))

	// Use community field (trimmed zeros) as the edge ID.
	edgeID := strings.TrimRight(string(hdr.Community[:]), "\x00")
	edge := s.RegisterEdge(edgeID, addr.IP, addr.Port, edgeID, hdr.Sequence)

	// Determine if this is a registration (payload starts with "REGISTER") or heartbeat.
	isRegistration := strings.HasPrefix(msg, "REGISTER")
	isHeartbeat := (hdr.Flags == 1) || (msg == "HEARTBEAT")

	if isRegistration {
		log.Printf("Supernode: Received registration from edge %s", edgeID)
	} else if isHeartbeat {
		log.Printf("Supernode: Received heartbeat from edge %s", edgeID)
	} else {
		log.Printf("Supernode: Received data packet from edge %s: seq=%d, payloadLen=%d", edgeID, hdr.Sequence, len(payload))
		// Further payload processing could occur here.
	}

	// For registration, include the assigned virtual IP in the ACK.
	ackMsg := "ACK"
	if isRegistration {
		ackMsg = fmt.Sprintf("ACK %s", edge.VirtualIP.String())
	}

	if err := s.SendAck(addr, ackMsg); err != nil {
		log.Printf("Supernode: Failed to send ACK to %v: %v", addr, err)
	}
}

// SendAck sends an ACK message back to the specified address.
func (s *Supernode) SendAck(addr *net.UDPAddr, msg string) error {
	_, err := s.Conn.WriteToUDP([]byte(msg), addr)
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

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
	ID            string    // Unique edge identifier (as provided during registration)
	PublicIP      net.IP    // Edge's public IP address
	Port          int       // Edge's UDP port
	Community     string    // Community membership (set during registration)
	VirtualIP     net.IP    // Assigned virtual IP address
	LastHeartbeat time.Time // Last time a packet was received from this edge
	LastSequence  uint16    // Last sequence number received
}

// Supernode holds the registry of edges and a UDP connection.
type Supernode struct {
	mu    sync.RWMutex
	edges map[string]*Edge // keyed by edge ID
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
// id is the unique edge identifier extracted from the registration payload.
func (s *Supernode) RegisterEdge(id string, publicIP net.IP, port int, community string, seq uint16) *Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, exists := s.edges[id]
	if !exists {
		// Generate a virtual IP address.
		// For demonstration, we use sequence modulo 254 plus one for the last octet on 10.0.0.0/8.
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
		log.Printf("Supernode: New edge registered: id=%s, community=%s, assigned VIP=%s", id, community, vip.String())
	} else {
		// For non-registration packets, we require that the community matches.
		if community != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q", id, community, edge.Community)
			return nil
		}
		edge.PublicIP = publicIP
		edge.Port = port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		log.Printf("Supernode: Edge updated: id=%s, community=%s", id, edge.Community)
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
// updates the edge registry, and processes the packet payload.
// For registration messages, it extracts the edge ID from the payload.
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

	var edgeID string
	var isRegistration bool
	if strings.HasPrefix(msg, "REGISTER") {
		isRegistration = true
		parts := strings.Fields(msg)
		if len(parts) >= 2 {
			edgeID = parts[1]
		} else {
			log.Printf("Supernode: Malformed registration message from %v: %q", addr, msg)
			return
		}
	} else {
		// For non-registration messages, look up an existing edge by matching remote address.
		s.mu.RLock()
		found := false
		for _, edge := range s.edges {
			if edge.PublicIP.Equal(addr.IP) && edge.Port == addr.Port {
				edgeID = edge.ID
				found = true
				break
			}
		}
		s.mu.RUnlock()
		if !found {
			log.Printf("Supernode: Received packet from unknown edge at %v; dropping", addr)
			return
		}
	}

	// Use the community field from the header.
	community := strings.TrimRight(string(hdr.Community[:]), "\x00")
	// Verify that the registration (if present) matches the header's community.
	if isRegistration {
		// Use the community provided in the header for registration.
		// (Edge payload contains edgeID; community is in the header.)
	} else {
		// For non-registration messages, ensure that the packet's community matches the registered edge.
		s.mu.RLock()
		if regEdge, ok := s.edges[edgeID]; ok {
			if community != regEdge.Community {
				s.mu.RUnlock()
				log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping",
					edgeID, community, regEdge.Community)
				return
			}
		}
		s.mu.RUnlock()
	}

	// Register or update the edge.
	edge := s.RegisterEdge(edgeID, addr.IP, addr.Port, community, hdr.Sequence)
	if edge == nil {
		// Registration failed due to mismatch.
		return
	}

	isHeartbeat := (hdr.Flags == 1) || (msg == "HEARTBEAT")
	if isHeartbeat {
		log.Printf("Supernode: Received heartbeat from edge %s", edgeID)
	} else {
		log.Printf("Supernode: Received data packet from edge %s: seq=%d, payloadLen=%d", edgeID, hdr.Sequence, len(payload))
		// Further payload processing can be implemented here.
	}

	// For registration messages, include the assigned virtual IP in the ACK.
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

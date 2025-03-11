// Package supernode maintains a registry of registered edges (peers)
// and processes incoming packets from edges using protocol framing.
// It provides functions to register/update edges, cleanup stale entries,
// and includes extensive debug logging.
package supernode

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"n2n-go/pkg/protocol"
)

const debug = false // set to true to enable verbose debug output

// Edge represents a registered edge.
type Edge struct {
	ID            string    // Unique edge identifier (from registration payload)
	PublicIP      net.IP    // Edge's public IP address
	Port          int       // Edge's UDP port
	Community     string    // Registered community membership
	VirtualIP     net.IP    // Assigned virtual IP address
	LastHeartbeat time.Time // Last heartbeat time
	LastSequence  uint16    // Last sequence number received
}

// VIPPool manages VIP allocation for a specific community.
type VIPPool struct {
	mu   sync.Mutex
	used map[uint8]string // maps last octet to edge ID
}

// allocate allocates a VIP for the given edge ID in the pool.
func (pool *VIPPool) allocate(edgeID string) (net.IP, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for octet := uint8(1); octet < 255; octet++ {
		if _, used := pool.used[octet]; !used {
			pool.used[octet] = edgeID
			return net.IPv4(10, 0, 0, octet), nil
		}
	}
	return nil, fmt.Errorf("no available VIP addresses")
}

// free releases the VIP assigned to the given edge ID.
func (pool *VIPPool) free(edgeID string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for octet, id := range pool.used {
		if id == edgeID {
			delete(pool.used, octet)
			return
		}
	}
}

// Supernode holds the registry of edges, VIP pools, and a UDP connection.
type Supernode struct {
	mu       sync.RWMutex
	edges    map[string]*Edge    // keyed by edge ID
	vipPools map[string]*VIPPool // keyed by community
	Conn     *net.UDPConn
}

// NewSupernode creates a new Supernode instance using an established UDP connection.
func NewSupernode(conn *net.UDPConn) *Supernode {
	return &Supernode{
		edges:    make(map[string]*Edge),
		vipPools: make(map[string]*VIPPool),
		Conn:     conn,
	}
}

// getOrCreateVIPPoolLocked returns the VIP pool for the given community.
// It assumes that s.mu is already held.
func (s *Supernode) getOrCreateVIPPoolLocked(community string) *VIPPool {
	pool, exists := s.vipPools[community]
	if !exists {
		pool = &VIPPool{
			used: make(map[uint8]string),
		}
		s.vipPools[community] = pool
	}
	return pool
}

// getOrCreateVIPPool returns the VIP pool for the given community, creating it if needed.
func (s *Supernode) getOrCreateVIPPool(community string) *VIPPool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getOrCreateVIPPoolLocked(community)
}

// RegisterEdge adds or updates an edge record. For registration messages,
// the VIP is allocated from the appropriate community pool.
func (s *Supernode) RegisterEdge(id string, publicIP net.IP, port int, community string, seq uint16, isReg bool) *Edge {
	s.mu.Lock()
	defer s.mu.Unlock()

	edge, exists := s.edges[id]
	if !exists {
		var vip net.IP
		if isReg { // allocate VIP only for registration messages
			pool := s.getOrCreateVIPPoolLocked(community)
			var err error
			vip, err = pool.allocate(id)
			if err != nil {
				log.Printf("Supernode: VIP allocation failed for edge %s: %v", id, err)
				return nil
			}
		}
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
		log.Printf("Supernode: New edge registered: id=%s, community=%s, assigned VIP=%s", id, community, vip)
	} else {
		if community != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", id, community, edge.Community)
			return nil
		}
		edge.PublicIP = publicIP
		edge.Port = port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		log.Printf("Supernode: Edge updated: id=%s, community=%s", id, community)
	}
	return edge
}

// UnregisterEdge removes an edge from the registry and frees its VIP.
func (s *Supernode) UnregisterEdge(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, exists := s.edges[id]
	if !exists {
		return
	}
	if pool, ok := s.vipPools[edge.Community]; ok {
		pool.free(id)
	}
	delete(s.edges, id)
	log.Printf("Supernode: Edge %s unregistered and VIP freed", id)
}

// CleanupStaleEdges removes edge records that haven't been updated within the expiry duration.
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			if pool, ok := s.vipPools[edge.Community]; ok {
				pool.free(id)
			}
			delete(s.edges, id)
			log.Printf("Supernode: Edge %s removed due to stale heartbeat", id)
		}
	}
}

// ProcessPacket parses an incoming packet using the protocol package,
// updates the edge registry, and processes the payload.
// It handles registration ("REGISTER <edgeID>") and unregistration ("UNREGISTER <edgeID>") messages.
func (s *Supernode) ProcessPacket(packet []byte, addr *net.UDPAddr) {
	// Log raw packet for debugging.
	if debug {
		log.Printf("Supernode: Raw packet from %v: %s", addr, hexDump(packet))
	}

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
	log.Printf("Supernode: Payload from %v: %q", addr, msg)

	var edgeID string
	isReg := false
	isUnreg := false
	if strings.HasPrefix(msg, "REGISTER") {
		isReg = true
		parts := strings.Fields(msg)
		if len(parts) >= 2 {
			edgeID = parts[1]
		} else {
			log.Printf("Supernode: Malformed registration message from %v: %q", addr, msg)
			return
		}
	} else if strings.HasPrefix(msg, "UNREGISTER") {
		isUnreg = true
		parts := strings.Fields(msg)
		if len(parts) >= 2 {
			edgeID = parts[1]
		} else {
			log.Printf("Supernode: Malformed unregister message from %v: %q", addr, msg)
			return
		}
	} else {
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
	community := strings.TrimRight(string(hdr.Community[:]), "\x00")
	log.Printf("Supernode: Extracted edgeID=%q, community=%q from packet", edgeID, community)

	if isUnreg {
		s.UnregisterEdge(edgeID)
		if err := s.SendAck(addr, "ACK UNREGISTER"); err != nil {
			log.Printf("Supernode: Failed to send UNREGISTER ACK to %v: %v", addr, err)
		}
		return
	}

	edge := s.RegisterEdge(edgeID, addr.IP, addr.Port, community, hdr.Sequence, isReg)
	if edge == nil {
		s.SendAck(addr, "ERR Registration failed")
		return
	}

	isHeartbeat := (hdr.Flags == 1) || strings.HasPrefix(msg, "HEARTBEAT")
	if isHeartbeat {
		log.Printf("Supernode: Received heartbeat from edge %s", edgeID)
	} else {
		log.Printf("Supernode: Received data packet from edge %s: seq=%d, payloadLen=%d", edgeID, hdr.Sequence, len(payload))
	}

	ackMsg := "ACK"
	if isReg {
		ackMsg = fmt.Sprintf("ACK %s", edge.VirtualIP.String())
	}
	if err := s.SendAck(addr, ackMsg); err != nil {
		log.Printf("Supernode: Failed to send ACK to %v: %v", addr, err)
	}

	if debug {
		log.Printf("Supernode: End ProcessPacket for edgeID=%q", edgeID)
	}
}

// hexDump returns a hex dump of the given data.
func hexDump(data []byte) string {
	const width = 16
	var result strings.Builder
	for i := 0; i < len(data); i += width {
		end := i + width
		if end > len(data) {
			end = len(data)
		}
		result.WriteString(fmt.Sprintf("%04x: %s\n", i, hexEncode(data[i:end])))
	}
	return result.String()
}

// hexEncode returns a hex-encoded string for the provided byte slice.
func hexEncode(data []byte) string {
	return strings.ToUpper(hex.EncodeToString(data))
}

// SendAck sends an ACK message back to the specified address.
func (s *Supernode) SendAck(addr *net.UDPAddr, msg string) error {
	log.Printf("Supernode: Sending ACK to %v: %s", addr, msg)
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
		if debug {
			log.Printf("Supernode: Received %d bytes from %v", n, addr)
		}
		go s.ProcessPacket(buf[:n], addr)
	}
}

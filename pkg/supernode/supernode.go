// Package supernode maintains a registry of registered edges (peers),
// processes incoming packets using the refined protocol header, forwards
// data packets selectively, and periodically cleans up stale edge records.
// This version retains our previous features such as dynamic VIP allocation per community.
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

const debug = true

// VIPPool manages VIP allocation for a specific community.
type VIPPool struct {
	mu   sync.Mutex
	used map[uint8]string // maps last octet to edge ID
}

// NewVIPPool creates a new VIPPool.
func NewVIPPool() *VIPPool {
	return &VIPPool{used: make(map[uint8]string)}
}

// Allocate assigns a free VIP (in 10.0.0.x) for a new edge.
func (pool *VIPPool) Allocate(edgeID string) (net.IP, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	// For example, assign addresses in range 10.0.0.2 - 10.0.0.254 (reserve .1 for supernode)
	for octet := uint8(2); octet < 255; octet++ {
		if _, ok := pool.used[octet]; !ok {
			pool.used[octet] = edgeID
			return net.IPv4(10, 0, 0, octet), nil
		}
	}
	return nil, fmt.Errorf("no available VIP addresses")
}

// Free releases the VIP assigned to the given edge.
func (pool *VIPPool) Free(edgeID string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for octet, id := range pool.used {
		if id == edgeID {
			delete(pool.used, octet)
			return
		}
	}
}

// Edge represents a registered edge.
type Edge struct {
	ID            string    // Unique edge identifier (from header.SourceID)
	PublicIP      net.IP    // Edge's public IP address
	Port          int       // Edge's UDP port
	Community     string    // Registered community
	VirtualIP     net.IP    // Assigned VIP
	LastHeartbeat time.Time // Timestamp of last heartbeat
	LastSequence  uint16    // Last sequence number seen
}

// Supernode holds state for registered edges, VIP pools, and a UDP connection.
type Supernode struct {
	mu       sync.RWMutex
	edges    map[string]*Edge    // keyed by edge ID
	vipPools map[string]*VIPPool // keyed by community
	Conn     *net.UDPConn
	// Expiry duration for cleaning up stale edges.
	edgeExpiry time.Duration
}

// NewSupernode creates a new Supernode instance using an established UDP connection.
func NewSupernode(conn *net.UDPConn, expiry time.Duration) *Supernode {
	s := &Supernode{
		edges:      make(map[string]*Edge),
		vipPools:   make(map[string]*VIPPool),
		Conn:       conn,
		edgeExpiry: expiry,
	}
	// Start periodic cleanup of stale edges.
	go s.periodicCleanup()
	return s
}

// getVIPPool retrieves (or creates) a VIP pool for the given community.
func (s *Supernode) getVIPPool(community string) *VIPPool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pool, exists := s.vipPools[community]
	if !exists {
		pool = NewVIPPool()
		s.vipPools[community] = pool
	}
	return pool
}

// RegisterEdge creates or updates an edge record.
// For registration packets, it allocates a VIP from the pool.
func (s *Supernode) RegisterEdge(srcID, community string, addr *net.UDPAddr, seq uint16, isReg bool) *Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, exists := s.edges[srcID]
	if !exists {
		var vip net.IP
		if isReg {
			pool := s.getVIPPool(community)
			var err error
			vip, err = pool.Allocate(srcID)
			if err != nil {
				log.Printf("Supernode: VIP allocation failed for edge %s: %v", srcID, err)
				return nil
			}
		}
		edge = &Edge{
			ID:            srcID,
			PublicIP:      addr.IP,
			Port:          addr.Port,
			Community:     community,
			VirtualIP:     vip,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
		}
		s.edges[srcID] = edge
		log.Printf("Supernode: New edge registered: id=%s, community=%s, assigned VIP=%s", srcID, community, vip)
	} else {
		if community != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", srcID, community, edge.Community)
			return nil
		}
		edge.PublicIP = addr.IP
		edge.Port = addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		log.Printf("Supernode: Edge updated: id=%s, community=%s", srcID, community)
	}
	return edge
}

// UnregisterEdge removes an edge and frees its VIP.
func (s *Supernode) UnregisterEdge(srcID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if edge, exists := s.edges[srcID]; exists {
		if pool, ok := s.vipPools[edge.Community]; ok {
			pool.Free(srcID)
		}
		delete(s.edges, srcID)
		log.Printf("Supernode: Edge %s unregistered (VIP %s freed)", srcID, edge.VirtualIP.String())
	}
}

// CleanupStaleEdges removes edge records that haven't been updated within the expiry duration.
func (s *Supernode) CleanupStaleEdges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > s.edgeExpiry {
			// Free VIP.
			if pool, ok := s.vipPools[edge.Community]; ok {
				pool.Free(id)
			}
			delete(s.edges, id)
			log.Printf("Supernode: Edge %s removed due to stale heartbeat", id)
		}
	}
}

// periodicCleanup runs CleanupStaleEdges periodically.
func (s *Supernode) periodicCleanup() {
	ticker := time.NewTicker(s.edgeExpiry / 2)
	defer ticker.Stop()
	for {
		<-ticker.C
		s.CleanupStaleEdges()
	}
}

// SendAck sends an ACK message back to the sender.
// For registration packets, ACK includes the assigned VIP.
func (s *Supernode) SendAck(addr *net.UDPAddr, msg string) error {
	log.Printf("Supernode: Sending ACK to %v: %s", addr, msg)
	_, err := s.Conn.WriteToUDP([]byte(msg), addr)
	return err
}

// ProcessPacket handles an incoming UDP packet.
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
	community := strings.TrimRight(string(hdr.Community[:]), "\x00")
	srcID := strings.TrimRight(string(hdr.SourceID[:]), "\x00")
	destID := strings.TrimRight(string(hdr.DestinationID[:]), "\x00")

	log.Printf("Supernode: Received packet: Type=%d, srcID=%q, destID=%q, community=%q, seq=%d, payloadLen=%d",
		hdr.PacketType, srcID, destID, community, hdr.Sequence, len(payload))

	switch hdr.PacketType {
	case protocol.TypeRegister:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, true)
		if edge == nil {
			s.SendAck(addr, "ERR Registration failed")
			return
		}
		ackMsg := fmt.Sprintf("ACK %s", edge.VirtualIP.String())
		s.SendAck(addr, ackMsg)
	case protocol.TypeUnregister:
		s.UnregisterEdge(srcID)
		// No ACK for unregister.
	case protocol.TypeHeartbeat:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false)
		if edge != nil {
			log.Printf("Supernode: Heartbeat received from edge %s", srcID)
		}
		s.SendAck(addr, "ACK")
	case protocol.TypeData:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false)
		if edge == nil {
			s.SendAck(addr, "ERR")
			return
		}
		log.Printf("Supernode: Data packet received from edge %s", srcID)
		s.mu.RLock()
		if destID != "" {
			if target, ok := s.edges[destID]; ok {
				if err := s.forwardPacket(packet, target); err != nil {
					log.Printf("Supernode: Failed to forward packet to %s: %v", destID, err)
				} else if debug {
					log.Printf("Supernode: Forwarded packet from %s to %s", srcID, destID)
				}
			}
		} else {
			for _, target := range s.edges {
				if target.Community == community && target.ID != srcID {
					if err := s.forwardPacket(packet, target); err != nil {
						log.Printf("Supernode: Failed to forward packet to %s: %v", target.ID, err)
					} else if debug {
						log.Printf("Supernode: Forwarded packet from %s to %s", srcID, target.ID)
					}
				}
			}
		}
		s.mu.RUnlock()
		s.SendAck(addr, "ACK")
	case protocol.TypeAck:
		log.Printf("Supernode: Received ACK from edge %s", srcID)
	default:
		log.Printf("Supernode: Unknown packet type %d from %v", hdr.PacketType, addr)
	}
}

func (s *Supernode) forwardPacket(packet []byte, target *Edge) error {
	addr := &net.UDPAddr{
		IP:   target.PublicIP,
		Port: target.Port,
	}
	if debug {
		log.Printf("Supernode: Forwarding packet to edge %s at %v", target.ID, addr)
	}
	_, err := s.Conn.WriteToUDP(packet, addr)
	return err
}

// Listen continuously reads from the UDP connection and processes packets.
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

// hexDump returns a hex dump of data.
func hexDump(data []byte) string {
	const width = 16
	var sb strings.Builder
	for i := 0; i < len(data); i += width {
		end := i + width
		if end > len(data) {
			end = len(data)
		}
		sb.WriteString(fmt.Sprintf("%04x: %s\n", i, strings.ToUpper(hex.EncodeToString(data[i:end]))))
	}
	return sb.String()
}

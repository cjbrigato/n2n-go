// Package supernode maintains registered edges, dynamic VIP pools,
// and processes incoming packets using the refined protocol header.
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

const debug = true // set to true for verbose logging

// VIPPool manages VIP allocation for a community.
type VIPPool struct {
	mu   sync.Mutex
	used map[uint8]string // maps last octet to edge ID
}

func NewVIPPool() *VIPPool {
	return &VIPPool{used: make(map[uint8]string)}
}

func (pool *VIPPool) Allocate(edgeID string) (net.IP, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	// Assign addresses in range 10.0.0.2 - 10.0.0.254 (reserve .1 for supernode).
	for octet := uint8(2); octet < 255; octet++ {
		if _, ok := pool.used[octet]; !ok {
			pool.used[octet] = edgeID
			return net.IPv4(10, 0, 0, octet), nil
		}
	}
	return nil, fmt.Errorf("no available VIP addresses")
}

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
	ID            string // from header.SourceID
	PublicIP      net.IP
	Port          int
	Community     string
	VirtualIP     net.IP
	LastHeartbeat time.Time
	LastSequence  uint16
}

// Supernode holds registered edges, VIP pools, and a UDP connection.
type Supernode struct {
	mu       sync.RWMutex
	edges    map[string]*Edge    // keyed by edge ID
	vipPools map[string]*VIPPool // keyed by community
	Conn     *net.UDPConn

	// Edge expiry duration.
	expiry time.Duration
}

func NewSupernode(conn *net.UDPConn, expiry time.Duration) *Supernode {
	sn := &Supernode{
		edges:    make(map[string]*Edge),
		vipPools: make(map[string]*VIPPool),
		Conn:     conn,
		expiry:   expiry,
	}
	// Start periodic cleanup.
	go sn.periodicCleanup()
	return sn
}

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
				if debug {
					log.Printf("Supernode: VIP allocation failed for edge %s: %v", srcID, err)
				}
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
		if debug {
			log.Printf("Supernode: New edge registered: id=%s, community=%s, assigned VIP=%s", srcID, community, vip)
		}
	} else {
		if community != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", srcID, community, edge.Community)
			return nil
		}
		edge.PublicIP = addr.IP
		edge.Port = addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		if debug {
			log.Printf("Supernode: Edge updated: id=%s, community=%s", srcID, community)
		}
	}
	return edge
}

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

func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			if pool, ok := s.vipPools[edge.Community]; ok {
				pool.Free(id)
			}
			delete(s.edges, id)
			log.Printf("Supernode: Edge %s removed due to stale heartbeat", id)
		}
	}
}

func (s *Supernode) periodicCleanup() {
	ticker := time.NewTicker(s.expiry / 2)
	defer ticker.Stop()
	for range ticker.C {
		s.CleanupStaleEdges(s.expiry)
	}
}

func (s *Supernode) SendAck(addr *net.UDPAddr, msg string) error {
	if debug {
		log.Printf("Supernode: Sending ACK to %v: %s", addr, msg)
	}
	_, err := s.Conn.WriteToUDP([]byte(msg), addr)
	return err
}

func (s *Supernode) ProcessPacket(packet []byte, addr *net.UDPAddr) {
	if len(packet) < protocol.TotalHeaderSize {
		log.Printf("Supernode: Packet too short from %v", addr)
		return
	}
	var hdr protocol.Header
	if err := hdr.UnmarshalBinary(packet[:protocol.TotalHeaderSize]); err != nil {
		log.Printf("Supernode: Failed to unmarshal header from %v: %v", addr, err)
		return
	}
	payload := packet[protocol.TotalHeaderSize:]
	community := strings.TrimRight(string(hdr.Community[:]), "\x00")
	srcID := strings.TrimRight(string(hdr.SourceID[:]), "\x00")
	destID := strings.TrimRight(string(hdr.DestinationID[:]), "\x00")

	if debug {
		log.Printf("Supernode: Received packet: Type=%d, srcID=%q, destID=%q, community=%q, seq=%d, payloadLen=%d",
			hdr.PacketType, srcID, destID, community, hdr.Sequence, len(payload))
	}

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
	case protocol.TypeHeartbeat:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false)
		if edge != nil && debug {
			log.Printf("Supernode: Heartbeat received from edge %s", srcID)
		}
		s.SendAck(addr, "ACK")
	case protocol.TypeData:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false)
		if edge == nil {
			s.SendAck(addr, "ERR")
			return
		}
		if debug {
			log.Printf("Supernode: Data packet received from edge %s", srcID)
		}
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
		if debug {
			log.Printf("Supernode: Received ACK from edge %s", srcID)
		}
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

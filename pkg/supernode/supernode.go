// Package supernode maintains registered edges, VIP pools, and MAC-to-edge mappings,
// and processes incoming packets using the refined protocol header.
package supernode

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"n2n-go/pkg/protocol"
)

const debug = false // set to true for verbose logging
const (
	communitySubnetFrom = "10.128.0.0" // Communities will be allocated upper subnets half of this
	communitySubnetCIDR = 24
)

// Edge represents a registered edge.
type Edge struct {
	ID            string // from header.SourceID
	PublicIP      net.IP
	Port          int
	Community     string
	VirtualIP     netip.Addr
	VNetMaskLen   int
	LastHeartbeat time.Time
	LastSequence  uint16
	// MAC address provided during registration.
	MACAddr string
}

type GlobalID struct {
	id        string
	community string
}

func NewGlobalID(id string, community string) *GlobalID {
	return &GlobalID{
		id:        id,
		community: community,
	}
}

func ParseGlobalID(s string) (*GlobalID, error) {
	parts := strings.Split(s, ".communities/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("cannot parse invalid GID")
	}
	return NewGlobalID(parts[1], parts[0]), nil
}

func (gid *GlobalID) String() string {
	return fmt.Sprintf("%s.communities/%s", gid.community, gid.id)
}

func (e *Edge) GID() *GlobalID {
	return NewGlobalID(e.ID, e.Community)
}

// Supernode holds registered edges, VIP pools, and a MAC-to-edge mapping.
type Supernode struct {
	edgeMu sync.RWMutex       // protects edges
	edges  map[GlobalID]*Edge // keyed by edge GlobalID

	netAllocator *NetworkAllocator

	comMu       sync.RWMutex
	communities map[string]*Community

	/*vipMu    sync.RWMutex         // protects vipPools
	vipPools map[string]*AddrPool // keyed by community*/

	macMu     sync.RWMutex        // protects macToEdge mapping
	macToEdge map[string]GlobalID // maps normalized MAC string to edge ID

	Conn            *net.UDPConn
	expiry          time.Duration // edge expiry duration
	cleanupInterval time.Duration // expiry Checking ticker
}

func NewSupernode(conn *net.UDPConn, expiry time.Duration, cleanupInterval time.Duration) *Supernode {
	netAllocator := NewNetworkAllocator(net.ParseIP(communitySubnetFrom), net.CIDRMask(communitySubnetCIDR, 32))
	sn := &Supernode{
		edges:        make(map[GlobalID]*Edge),
		netAllocator: netAllocator,
		communities:  make(map[string]*Community),
		//vipPools:        make(map[string]*AddrPool),
		macToEdge:       make(map[string]GlobalID),
		Conn:            conn,
		expiry:          expiry,
		cleanupInterval: cleanupInterval,
	}
	go sn.cleanupRoutine()
	return sn
}

func (s *Supernode) GetCommunity(community string, create bool) (*Community, error) {
	s.comMu.Lock()
	defer s.comMu.Unlock()
	c, exists := s.communities[community]
	if !exists {
		if !create {
			return nil, fmt.Errorf("unknown community")
		}
		prefix, err := s.netAllocator.ProposeVirtualNetwork(community)
		if err != nil {
			return nil, err
		}
		c = NewCommunity(community, prefix)
		s.communities[community] = c
	}
	return c, nil
}

// RegisterEdge now expects payload to contain "REGISTER <edgeID> <tapMAC>"
// The tapMAC is expected in its standard string representation (e.g., "aa:bb:cc:dd:ee:ff")
// We convert it to a net.HardwareAddr.
func (s *Supernode) RegisterEdge(srcID, community string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) (*Edge, error) {

	s.edgeMu.Lock()
	defer s.edgeMu.Unlock()

	gid := NewGlobalID(srcID, community)
	edge, exists := s.edges[*gid]
	if !exists && !isReg {
		return nil, fmt.Errorf("unknown edge with !isReg. Droping")
	}

	// from there (exists || isReg) is always true, so the community either exists or should be created
	cm, err := s.GetCommunity(community, isReg)
	if err != nil {
		return nil, err
	}

	// transfer registering to communityScope
	edge, err = cm.EdgeUpdate(srcID, addr, seq, isReg, payload)
	if err != nil {
		return nil, err
	}

	s.edges[*gid] = edge
	return edge, nil
}

func (s *Supernode) UnregisterEdge(srcID string, community string) {
	s.edgeMu.Lock()
	defer s.edgeMu.Unlock()
	gid := *NewGlobalID(srcID, community)
	if edge, exists := s.edges[gid]; exists {
		cm, err := s.GetCommunity(community, false)
		if err != nil {
			log.Printf("%v", err)
			return
		}
		if cm.Unregister(srcID) {
			s.macMu.Lock()
			delete(s.macToEdge, edge.MACAddr)
			s.macMu.Unlock()
			delete(s.edges, gid)
		}
	}
}

// CleanupStaleEdges and periodicCleanup remain unchanged.
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	var stale []GlobalID
	now := time.Now()
	s.edgeMu.RLock()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			stale = append(stale, id)
		}
	}
	s.edgeMu.RUnlock()
	if len(stale) > 0 {
		//s.edgeMu.Lock()
		for _, id := range stale {
			s.UnregisterEdge(id.id, id.community)
		}
		//s.edgeMu.Unlock()
	}
}

func (s *Supernode) cleanupRoutine() {
	ticker := time.NewTicker(s.cleanupInterval)
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
	gid := *NewGlobalID(srcID, community)
	// Interpret Destination field as destination MAC address.
	// Here we extract the first 6 bytes as the MAC.
	destMACRaw := hdr.DestinationID[0:6]
	var destMAC string
	// If all bytes are zero, treat as empty.
	empty := true
	for _, b := range destMACRaw {
		if b != 0 {
			empty = false
			break
		}
	}
	if !empty {
		destMAC = net.HardwareAddr(destMACRaw).String()
	}

	if debug {
		log.Printf("Supernode: Received packet: Type=%d, srcID=%q, destMAC=%q, community=%q, seq=%d, payloadLen=%d",
			hdr.PacketType, srcID, destMAC, community, hdr.Sequence, len(payload))
	}

	switch hdr.PacketType {
	case protocol.TypeRegister:
		payloadStr := strings.TrimSpace(string(payload))
		edge, err := s.RegisterEdge(srcID, community, addr, hdr.Sequence, true, payloadStr)
		if edge == nil || err != nil {
			s.SendAck(addr, "ERR Registration failed")
			return
		}
		ackMsg := fmt.Sprintf("ACK %s %d", edge.VirtualIP.String(), edge.VNetMaskLen)
		s.SendAck(addr, ackMsg)
	case protocol.TypeUnregister:
		s.UnregisterEdge(srcID, community)
	case protocol.TypeHeartbeat:
		edge, err := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false, "")
		if edge != nil && err == nil {
			log.Printf("Supernode: !edge.heartbeat: id=%s, community=%s, VIP=%s, MAC=%s, ", srcID, edge.Community, edge.VirtualIP.String(), edge.MACAddr)
		}
		s.SendAck(addr, "ACK")
	case protocol.TypeData:
		// Look up the sender edge. If not registered, ignore the packet.
		s.edgeMu.RLock()
		senderEdge, exists := s.edges[gid]
		s.edgeMu.RUnlock()
		if !exists {
			log.Printf("Supernode: Received data packet from unregistered edge %s; dropping packet", srcID)
			return
		}
		// Update sender's heartbeat and sequence.
		s.edgeMu.Lock()
		senderEdge.LastHeartbeat = time.Now()
		senderEdge.LastSequence = hdr.Sequence
		s.edgeMu.Unlock()

		if debug {
			log.Printf("Supernode: Data packet received from edge %s", srcID)
		}

		// Use the destination MAC address from the header.
		if destMAC != "" {
			s.macMu.RLock()
			targetEdgeID, exists := s.macToEdge[destMAC]
			s.macMu.RUnlock()
			if exists {
				s.edgeMu.RLock()
				target, ok := s.edges[targetEdgeID]
				s.edgeMu.RUnlock()
				if ok {
					if err := s.forwardPacket(packet, target); err != nil {
						log.Printf("Supernode: Failed to forward packet to edge %s: %v", target.ID, err)
					} else if debug {
						log.Printf("Supernode: Forwarded packet to edge %s", target.ID)
					}
					// Forwarded to specific target; no ACK for data.
					return
				}
			}
			if debug {
				log.Printf("Supernode: Destination MAC %s not found. Fallbacking to broadcast for community %s", destMAC, community)
			}
			s.broadcast(packet, community, srcID)
		} else {
			if debug {
				log.Printf("Supernode: No destination MAC provided. Fallbacking to broadcast for community %s", community)
			}
			s.broadcast(packet, community, srcID)
		}
		// Do not send an ACK for data packets.
	case protocol.TypeAck:
		if debug {
			log.Printf("Supernode: Received ACK from edge %s", srcID)
		}
	default:
		log.Printf("Supernode: Unknown packet type %d from %v", hdr.PacketType, addr)
	}
}

func (s *Supernode) forwardPacket(packet []byte, target *Edge) error {
	addr := &net.UDPAddr{IP: target.PublicIP, Port: target.Port}
	if debug {
		log.Printf("Supernode: Forwarding packet to edge %s at %v", target.ID, addr)
	}
	_, err := s.Conn.WriteToUDP(packet, addr)
	return err
}

// broadcast sends the packet to all edges in the same community (except sender).
func (s *Supernode) broadcast(packet []byte, community, senderID string) {
	s.edgeMu.RLock()
	defer s.edgeMu.RUnlock()
	for _, target := range s.edges {
		if target.Community == community && target.ID != senderID {
			if err := s.forwardPacket(packet, target); err != nil {
				log.Printf("Supernode: Failed to forward packet to edge %s: %v", target.ID, err)
			} else if debug {
				log.Printf("Supernode: Broadcasted packet from %s to edge %s", senderID, target.ID)
			}
		}
	}
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

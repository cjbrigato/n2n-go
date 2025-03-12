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

// Supernode holds registered edges, VIP pools, and a MAC-to-edge mapping.
type Supernode struct {
	edgeMu sync.RWMutex     // protects edges
	edges  map[string]*Edge // keyed by edge ID

	netAllocator *NetworkAllocator

	vipMu    sync.RWMutex        // protects vipPools
	vipPools map[string]*VIPPool // keyed by community

	macMu     sync.RWMutex      // protects macToEdge mapping
	macToEdge map[string]string // maps normalized MAC string to edge ID

	Conn            *net.UDPConn
	expiry          time.Duration // edge expiry duration
	cleanupInterval time.Duration // expiry Checking ticker
}

func NewSupernode(conn *net.UDPConn, expiry time.Duration, cleanupInterval time.Duration) *Supernode {
	netAllocator := NewNetworkAllocator(net.ParseIP(communitySubnetFrom), net.CIDRMask(communitySubnetCIDR, 32))
	sn := &Supernode{
		edges:           make(map[string]*Edge),
		netAllocator:    netAllocator,
		vipPools:        make(map[string]*VIPPool),
		macToEdge:       make(map[string]string),
		Conn:            conn,
		expiry:          expiry,
		cleanupInterval: cleanupInterval,
	}
	go sn.cleanupRoutine()
	return sn
}

func (s *Supernode) getVIPPool(community string) (*VIPPool, error) {
	s.vipMu.Lock()
	defer s.vipMu.Unlock()
	pool, exists := s.vipPools[community]
	if !exists {
		prefix, err := s.netAllocator.ProposeVirtualNetwork(community)
		if err != nil {
			return nil, err
		}
		pool = NewVIPPool(prefix)
		s.vipPools[community] = pool
	}
	return pool, nil
}

// RegisterEdge now expects payload to contain "REGISTER <edgeID> <tapMAC>"
// The tapMAC is expected in its standard string representation (e.g., "aa:bb:cc:dd:ee:ff")
// We convert it to a net.HardwareAddr.
func (s *Supernode) RegisterEdge(srcID, community string, addr *net.UDPAddr, seq uint16, isReg bool, payload string) *Edge {
	s.edgeMu.Lock()
	defer s.edgeMu.Unlock()
	var macAddr net.HardwareAddr
	if isReg {
		parts := strings.Fields(payload)
		if len(parts) >= 3 {
			// Parse the MAC address.
			mac, err := net.ParseMAC(parts[2])
			if err != nil {
				log.Printf("Supernode: Failed to parse MAC address %s: %v", parts[2], err)
			} else {
				macAddr = mac
			}
		} else {
			log.Printf("Supernode: Registration payload format invalid: %s", payload)
		}
	}
	edge, exists := s.edges[srcID]
	if !exists {
		var vip netip.Addr
		var masklen int
		if isReg {
			pool, err := s.getVIPPool(community)
			if err != nil {
				if debug {
					log.Printf("Supernode: VIP getVIPPool failed for community %s: %v", community, err)
				}
			} else {
				vip, masklen, err = pool.Allocate(srcID)
				if err != nil {
					if debug {
						log.Printf("Supernode: VIP allocation failed for edge %s: %v", srcID, err)
					}
					return nil
				}
			}
		}
		edge = &Edge{
			ID:            srcID,
			PublicIP:      addr.IP,
			Port:          addr.Port,
			Community:     community,
			VirtualIP:     vip,
			VNetMaskLen:   masklen,
			LastHeartbeat: time.Now(),
			LastSequence:  seq,
			MACAddr:       "", // We store MAC as empty string in Edge, but update mapping below.
		}
		s.edges[srcID] = edge
		if macAddr != nil {
			edge.MACAddr = macAddr.String()
			s.macMu.Lock()
			s.macToEdge[edge.MACAddr] = srcID // store edge ID keyed by MAC string
			s.macMu.Unlock()
		}
		log.Printf("Supernode: +edge.register: id=%s, community=%s, assigned VIP=%s, MAC=%s", srcID, community, vip, edge.MACAddr)
	} else {
		if community != edge.Community {
			log.Printf("Supernode: Community mismatch for edge %s: packet community %q vs registered %q; dropping", srcID, community, edge.Community)
			return nil
		}
		edge.PublicIP = addr.IP
		edge.Port = addr.Port
		edge.LastHeartbeat = time.Now()
		edge.LastSequence = seq
		if isReg && macAddr != nil {
			edge.MACAddr = macAddr.String()
			s.macMu.Lock()
			s.macToEdge[edge.MACAddr] = srcID
			s.macMu.Unlock()
		}
		if debug {
			log.Printf("Supernode: ~edge.update: id=%s, community=%s", srcID, community)
		}
	}
	return edge
}

func (s *Supernode) UnregisterEdge(srcID string) {
	s.edgeMu.Lock()
	defer s.edgeMu.Unlock()
	if edge, exists := s.edges[srcID]; exists {
		pool, err := s.getVIPPool(edge.Community)
		if err != nil {
			if debug {
				log.Printf("Supernode: VIP getVIPPool failed for community %s: %v", edge.Community, err)
			}
		} else {
			pool.Free(srcID)
		}
		if edge.MACAddr != "" {
			s.macMu.Lock()
			delete(s.macToEdge, edge.MACAddr)
			s.macMu.Unlock()
		}
		delete(s.edges, srcID)
		log.Printf("Supernode: -edge.unregister: id=%s, community=%s, freed VIP=%s, MAC=%s, ", srcID, edge.Community, edge.VirtualIP.String(), edge.MACAddr)
	}
}

// CleanupStaleEdges and periodicCleanup remain unchanged.
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {
	var stale []string
	now := time.Now()
	s.edgeMu.RLock()
	for id, edge := range s.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			stale = append(stale, id)
		}
	}
	s.edgeMu.RUnlock()
	if len(stale) > 0 {
		s.edgeMu.Lock()
		for _, id := range stale {
			if edge, exists := s.edges[id]; exists {
				pool, err := s.getVIPPool(edge.Community)
				if err != nil {
					if debug {
						log.Printf("Supernode: VIP getVIPPool failed for community %s: %v", edge.Community, err)
					}
				} else {
					pool.Free(id)
				}
				if edge.MACAddr != "" {
					s.macMu.Lock()
					delete(s.macToEdge, edge.MACAddr)
					s.macMu.Unlock()
				}
				delete(s.edges, id)
				log.Printf("Supernode: -edge.unregister (removed due to stale heartbeat): id=%s, community=%s, freed VIP=%s, MAC=%s, ", id, edge.Community, edge.VirtualIP.String(), edge.MACAddr)
			}
		}
		s.edgeMu.Unlock()
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
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, true, payloadStr)
		if edge == nil {
			s.SendAck(addr, "ERR Registration failed")
			return
		}
		ackMsg := fmt.Sprintf("ACK %s %d", edge.VirtualIP.String(), edge.VNetMaskLen)
		s.SendAck(addr, ackMsg)
	case protocol.TypeUnregister:
		s.UnregisterEdge(srcID)
	case protocol.TypeHeartbeat:
		edge := s.RegisterEdge(srcID, community, addr, hdr.Sequence, false, "")
		if edge != nil {
			log.Printf("Supernode: !edge.heartbeat: id=%s, community=%s, VIP=%s, MAC=%s, ", srcID, edge.Community, edge.VirtualIP.String(), edge.MACAddr)
		}
		s.SendAck(addr, "ACK")
	case protocol.TypeData:
		// Look up the sender edge. If not registered, ignore the packet.
		s.edgeMu.RLock()
		senderEdge, exists := s.edges[srcID]
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

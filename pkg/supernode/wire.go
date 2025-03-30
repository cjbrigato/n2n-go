package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"time"
)

func (s *Supernode) ForwardUnicast(r *protocol.RawMessage) error {
	cm, err := s.GetCommunity(r)
	if err != nil {
		return err
	}
	targetEdge, found := cm.GetEdge(r.DestMACAddr())
	if !found {
		return fmt.Errorf("%w: %s", ErrUnicastForwardFail, r.EdgeMACAddr())
	}

	if err := s.forwardPacket(r.RawPacket(), targetEdge); err != nil {
		s.stats.PacketsDropped.Add(1)
		return fmt.Errorf("Supernode: Failed to forward packet to edge %s: %v", targetEdge.MACAddr, err)
	}
	s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
	s.stats.PacketsForwarded.Add(1)
	return nil

}

func (s *Supernode) ForwardWithFallBack(r *protocol.RawMessage) error {
	cm, err := s.GetCommunity(r)
	if err != nil {
		return err
	}
	targetEdge, found := cm.GetEdge(r.DestMACAddr())
	if found {
		if err := s.forwardPacket(r.RawPacket(), targetEdge); err != nil {
			s.stats.PacketsDropped.Add(1)
			log.Printf("Supernode: Failed to forward packet to edge %s: %v", targetEdge.MACAddr, err)
		} else {
			s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
			s.stats.PacketsForwarded.Add(1)
			return nil
		}
	}
	s.debugLog("Unable to selectively forward packet orNo destination MAC provided. Broadcasting to community %s", cm.name)
	s.broadcast(r.RawPacket(), cm, r.EdgeMACAddr())
	return nil
}

// forwardPacket sends a packet to a specific edge
func (s *Supernode) forwardPacket(packet []byte, target *Edge) error {
	var err error
	if packet[0] == protocol.VersionV {
		packet, err = protocol.FlagPacketFromSupernode(packet)
		if err != nil {
			return err
		}
	}
	addr := target.UDPAddr()
	s.debugLog("Forwarding packet to edge %s at %v", target.MACAddr, addr)
	_, err = s.Conn.WriteToUDP(packet, addr)
	return err
}

// broadcast sends a packet to all edges in the same community except the sender
func (s *Supernode) broadcast(packet []byte, cm *Community, senderID string) {
	targets := cm.GetAllEdges()

	// Now send to all targets without holding the lock
	sentCount := 0
	for _, target := range targets {
		if target.MACAddr == senderID {
			continue
		} // Check if we need to convert the packet for this target
		if err := s.forwardPacket(packet, target); err != nil {
			log.Printf("Supernode: Failed to broadcast packet to edge %s: %v", target.MACAddr, err)
			s.stats.PacketsDropped.Add(1)
		} else {
			s.debugLog("Broadcasted packet from %s to edge %s", senderID, target.MACAddr)
			sentCount++
		}
	}

	if sentCount > 0 {
		s.stats.PacketsForwarded.Add(uint64(sentCount))
	}
}

func (s *Supernode) WritePacket(pt spec.PacketType, community string, dst net.HardwareAddr, payload []byte, addr *net.UDPAddr) error {

	header := s.SNHeader(pt, community, dst)

	_, err := s.Conn.WriteToUDP(protocol.PackProtoVDatagram(header, payload), addr)
	if err != nil {
		return fmt.Errorf(" failed to send packet: %w", err)
	}
	return nil
}

func (s *Supernode) SNHeader(pt spec.PacketType, community string, dst net.HardwareAddr) *protocol.ProtoVHeader {
	h := &protocol.ProtoVHeader{
		Version:     protocol.VersionV,
		TTL:         64,
		PacketType:  pt,
		Flags:       protocol.FlagFromSuperNode,
		Sequence:    0,
		CommunityID: protocol.HashCommunity(community),
		Timestamp:   uint32(time.Now().Unix()),
	}
	copy(h.SourceID[:], s.MacADDR()[:6])
	if dst != nil {
		copy(h.DestID[:], dst[:6])
	}
	return h
}

func (s *Supernode) SNStructHeader(p netstruct.PacketTyped, community string, dst net.HardwareAddr) *protocol.ProtoVHeader {
	return s.SNHeader(p.PacketType(), community, dst)
}

func (s *Supernode) SendStruct(p netstruct.PacketTyped, community string, src, dst net.HardwareAddr, addr *net.UDPAddr) error {

	header := s.SNStructHeader(p, community, dst)
	payload, err := protocol.Encode(p)
	if err != nil {
		return err
	}
	_, err = s.Conn.WriteToUDP(protocol.PackProtoVDatagram(header, payload), addr)
	if err != nil {
		return fmt.Errorf(" failed to send packet: %w", err)
	}
	return nil
}

func (s *Supernode) BroadcastStruct(p netstruct.PacketTyped, cm *Community, src, dst net.HardwareAddr, senderMac string) error {
	// Get buffer for full packet

	header := s.SNStructHeader(p, cm.Name(), dst)
	payload, err := protocol.Encode(p)
	if err != nil {
		return err
	}
	packet := protocol.PackProtoVDatagram(header, payload)

	s.broadcast(packet, cm, senderMac)
	return nil
}

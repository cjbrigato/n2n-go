package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"net"
	"time"
)

func (s *Supernode) handleP2PStateInfoMessage(r *protocol.RawMessage) error {
	p2pMsg, err := r.ToP2PStateInfoMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(p2pMsg.EdgeMACAddr, p2pMsg.CommunityHash)
	if err != nil {
		return err
	}
	err = cm.SetP2PInfosFor(p2pMsg.EdgeMACAddr, p2pMsg.PeerP2PInfos)
	if err != nil {
		return err
	}
	s.debugLog("Community:%s updated P2PInfosFor:%s", cm.Name(), p2pMsg.EdgeMACAddr)
	return nil
}

func (s *Supernode) handlePeerRequestMessage(r *protocol.RawMessage) error {
	peerReqMsg, err := r.ToPeerRequestMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(peerReqMsg.EdgeMACAddr, peerReqMsg.CommunityHash)
	if err != nil {
		return err
	}
	pil := cm.GetPeerInfoList(peerReqMsg.EdgeMACAddr, true)
	peerResponsePayload, err := pil.Encode()
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(peerReqMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	return s.WritePacket(protocol.TypePeerInfo, peerReqMsg.CommunityName, s.MacADDR(), nil, string(peerResponsePayload), target)
}

func (s *Supernode) handleAckMessage(r *protocol.RawMessage) error {
	ackMessage, err := r.ToAckMessage()
	if err != nil {
		return err
	}
	s.debugLog("Received ACK from edge %s", ackMessage.EdgeMACAddr)
	return nil
}

func (s *Supernode) handleLeasesInfosMEssage(r *protocol.RawMessage) error {
	leaseMsg, err := r.ToLeasesInfosMessage()
	if err != nil {
		return err
	}
	if !leaseMsg.IsRequest {
		return fmt.Errorf("Supernode do not handle non-request LeasesInfosMessage")
	}
	cm, err := s.GetCommunityForEdge(leaseMsg.EdgeMACAddr, leaseMsg.CommunityHash)
	if err != nil {
		return err
	}
	leases := cm.GetAllLease()
	infos := netstruct.LeasesInfos{
		IsRequest:     false,
		CommunityName: cm.Name(),
		Leases:        leases,
	}
	data, err := infos.Encode()
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(leaseMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	return s.WritePacket(protocol.TypeLeasesInfos, cm.Name(), s.MacADDR(), nil, string(data), target)
}

func (s *Supernode) handleUnregisterMessage(r *protocol.RawMessage) error {
	unRegMsg, err := r.ToUnregisterMessage()
	if err != nil {
		return err
	}
	return s.UnregisterEdge(unRegMsg.EdgeMACAddr, unRegMsg.CommunityHash)
}

func (s *Supernode) handleRegisterMessage(r *protocol.RawMessage) error {
	regMsg, err := r.ToRegisterMessage()
	if err != nil {
		return err
	}
	edge, cm, err := s.RegisterEdge(regMsg)
	if edge == nil || err != nil {
		log.Printf("Supernode: Registration failed for %s: %v", regMsg.EdgeMACAddr, err)
		s.SendAck(r.Addr, nil, "ERR Registration failed")
		s.stats.PacketsDropped.Add(1)
		return err
	}
	s.edgeMu.Lock()
	s.edgesByMAC[edge.MACAddr] = edge
	s.edgesBySocket[edge.UDPAddr().String()] = edge
	s.edgeMu.Unlock()
	pil := newPeerInfoEvent(p2p.TypeRegister, edge)
	peerInfoPayload, err := pil.Encode()
	if err != nil {
		log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
	} else {
		s.BroadcastPacket(protocol.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), regMsg.EdgeMACAddr)
	}

	ackMsg := fmt.Sprintf("ACK %s %d", edge.VirtualIP.String(), edge.VNetMaskLen)
	s.SendAck(r.Addr, edge, ackMsg)
	return nil
}

func (s *Supernode) handleHeartbeatMessage(r *protocol.RawMessage) error {
	heartbeatMsg, err := r.ToHeartbeatMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(heartbeatMsg.EdgeMACAddr, heartbeatMsg.CommunityHash)
	if err != nil {
		return err
	}
	s.stats.HeartbeatsReceived.Add(1)
	changed, err := cm.RefreshEdge(heartbeatMsg)
	if err != nil {
		return err
	}
	edge, exists := cm.GetEdge(heartbeatMsg.EdgeMACAddr)
	if exists {
		if changed {
			pil := newPeerInfoEvent(p2p.TypeRegister, edge)
			peerInfoPayload, err := pil.Encode()
			if err != nil {
				log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
			} else {
				s.BroadcastPacket(protocol.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), heartbeatMsg.EdgeMACAddr)
			}
		}
		return s.SendAck(r.Addr, edge, "ACK")
	}
	return nil
}

func (s *Supernode) handleP2PFullStateMessage(r *protocol.RawMessage) error {
	fsMsg, err := r.ToP2PFullStateMessage()
	if err != nil {
		return err
	}
	if !fsMsg.IsRequest {
		return fmt.Errorf("supernode does not handle non-requests P2PFullStatesMessage")
	}
	cm, err := s.GetCommunityForEdge(fsMsg.EdgeMACAddr, fsMsg.CommunityHash)
	if err != nil {
		return err
	}
	P2PFullState, err := cm.GetCommunityPeerP2PInfosDatas(fsMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	data, err := P2PFullState.Encode()
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(fsMsg.EdgeMACAddr)
	if err != nil {
		return err
	}
	//log.Printf("DEBUG: P2PFullStateMessageSize: %d bytes", len(string(data)))
	//return s.WriteFragments(protocol.TypeP2PFullState, s.MacADDR(), data, target)
	return s.WritePacket(protocol.TypeP2PFullState, cm.Name(), s.MacADDR(), nil, string(data), target)

}

// handleDataMessage processes a data packet
func (s *Supernode) handlePingMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {

	pingMsg, err := r.ToPingMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(pingMsg.EdgeMACAddr, pingMsg.CommunityHash)
	if err != nil {
		return err
	}
	senderEdge, found := cm.GetEdge(pingMsg.EdgeMACAddr)
	if !found {
		return fmt.Errorf("cannot find senderEdge %v in community for pingMsg packet handling", pingMsg.EdgeMACAddr)
	}
	var targetEdge *Edge
	if pingMsg.DestMACAddr != "" {
		te, found := cm.GetEdge(pingMsg.DestMACAddr)
		if found {
			targetEdge = te
		} else {

		}
	}
	// Update sender's heartbeat and sequence
	cm.edgeMu.Lock()
	if e, ok := cm.edges[pingMsg.EdgeMACAddr]; ok {
		e.LastHeartbeat = time.Now()
		e.LastSequence = pingMsg.RawMsg.Header.Sequence
	}
	cm.edgeMu.Unlock()

	s.debugLog("Ping packet received from edge %s", senderEdge.MACAddr)

	if targetEdge != nil {
		if err := s.forwardPacket(pingMsg.ToPacket(), targetEdge); err != nil {
			s.stats.PacketsDropped.Add(1)
			log.Printf("Supernode: Failed to forward pingMessage to edge %s: %v", targetEdge.MACAddr, err)
		} else {
			s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
			s.stats.PacketsForwarded.Add(1)
			return nil
		}
	}
	return nil
}

// handleDataMessage processes a data packet
func (s *Supernode) handleDataMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {

	dataMsg, err := r.ToDataMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunityForEdge(dataMsg.EdgeMACAddr, dataMsg.CommunityHash)
	if err != nil {
		return err
	}
	senderEdge, found := cm.GetEdge(dataMsg.EdgeMACAddr)
	if !found {
		return fmt.Errorf("cannot find senderEdge %v in community for data packet handling", dataMsg.EdgeMACAddr)
	}
	var targetEdge *Edge
	if dataMsg.DestMACAddr != "" {
		te, found := cm.GetEdge(dataMsg.DestMACAddr)
		if found {
			targetEdge = te
		} else {

		}
	}
	// Update sender's heartbeat and sequence
	cm.edgeMu.Lock()
	if e, ok := cm.edges[dataMsg.EdgeMACAddr]; ok {
		e.LastHeartbeat = time.Now()
		e.LastSequence = dataMsg.RawMsg.Header.Sequence
	}
	cm.edgeMu.Unlock()

	s.debugLog("Data packet received from edge %s", senderEdge.MACAddr)

	if targetEdge != nil {
		if err := s.forwardPacket(dataMsg.ToPacket(), targetEdge); err != nil {
			s.stats.PacketsDropped.Add(1)
			log.Printf("Supernode: Failed to forward packet to edge %s: %v", targetEdge.MACAddr, err)
		} else {
			s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
			s.stats.PacketsForwarded.Add(1)
			return nil
		}
	}
	s.debugLog("Unable to selectively forward packet orNo destination MAC provided. Broadcasting to community %s", cm.name)
	s.broadcast(dataMsg.ToPacket(), cm, senderEdge.MACAddr)
	return nil
}

func (s *Supernode) handleVFuze(packet []byte) {
	dst := net.HardwareAddr(packet[1:7])
	s.edgeMu.Lock()
	edge, ok := s.edgesByMAC[dst.String()]
	s.edgeMu.Unlock()
	if s.config.EnableVFuze {
		if ok {
			err := s.forwardPacket(packet, edge)
			if err != nil {
				log.Printf("Supernode: VersionVFuze error: %v", err)
			}
		} else {
			log.Printf("Supernode: cannot process VFuze packet: no edge found with this HardwareAddr %s", dst.String())
		}
	} else {
		log.Printf("Supernode: received a VFuze Protocol packet but configuration disabled it")
		if !ok {
			log.Printf("         -> cannot give hint about which edge has misconfiguration")
			log.Printf("           -> no edge found with requested HardwareAddr routing")
		} else {
			log.Printf("          -> Misconfigured edge Informations: Desc=%s, VIP=%s, MAC=%s", edge.Desc, edge.VirtualIP, edge.MACAddr)
		}
	}
}

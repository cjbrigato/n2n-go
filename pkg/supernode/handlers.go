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
	//p2pMsg, err := r.ToP2PStateInfoMessage()
	p2pMsg, err := protocol.ToMessage[*p2p.PeerP2PInfos](r)
	if err != nil {
		return err
	}
	cm, err := s.GetCommunity(p2pMsg)
	if err != nil {
		return err
	}
	err = cm.SetP2PInfosFor(p2pMsg.EdgeMACAddr(), p2pMsg.Msg)
	if err != nil {
		return err
	}
	s.debugLog("Community:%s updated P2PInfosFor:%s", cm.Name(), p2pMsg.EdgeMACAddr)
	return nil
}

func (s *Supernode) handlePeerRequestMessage(r *protocol.RawMessage) error {
	peerReqMsg, err := protocol.ToMessage[*netstruct.PeerListRequest](r)
	if err != nil {
		return err
	}
	cm, err := s.GetCommunity(peerReqMsg)
	if err != nil {
		return err
	}
	pil := cm.GetPeerInfoList(peerReqMsg.EdgeMACAddr(), true)
	target, err := cm.GetEdgeUDPAddr(peerReqMsg.EdgeMACAddr())
	if err != nil {
		return err
	}
	return s.SendStruct(pil, peerReqMsg.Msg.CommunityName, s.MacADDR(), nil, target)
}

func (s *Supernode) handleLeasesInfosMEssage(r *protocol.RawMessage) error {
	leaseMsg, err := protocol.ToMessage[*netstruct.LeasesInfos](r)
	if err != nil {
		return err
	}
	if !leaseMsg.Msg.IsRequest {
		return fmt.Errorf("Supernode do not handle non-request LeasesInfosMessage")
	}
	cm, err := s.GetCommunity(leaseMsg)
	if err != nil {
		return err
	}
	leases := cm.GetAllLease()
	infos := &netstruct.LeasesInfos{
		IsRequest:     false,
		CommunityName: cm.Name(),
		Leases:        leases,
	}
	target, err := cm.GetEdgeUDPAddr(leaseMsg.EdgeMACAddr())
	if err != nil {
		return err
	}
	return s.SendStruct(infos, cm.Name(), s.MacADDR(), nil, target)
}

func (s *Supernode) handleUnregisterMessage(r *protocol.RawMessage) error {
	unReg, err := protocol.ToMessage[*netstruct.UnregisterRequest](r)
	if err != nil {
		return err
	}
	return s.UnregisterEdge(unReg)
}

func (s *Supernode) handleRegisterMessage(r *protocol.RawMessage) error {
	reg, err := protocol.ToMessage[*netstruct.RegisterRequest](r)
	if err != nil {
		return err
	}
	edge, cm, err := s.RegisterEdge(reg)
	rresp := &netstruct.RegisterResponse{}
	if edge == nil || err != nil {
		log.Printf("Supernode: Registration failed for %s: %v", reg.Msg.EdgeMACAddr, err)
		rresp.IsRegisterOk = false
		s.SendStruct(rresp, reg.Msg.CommunityName, s.MacADDR(), nil, r.FromAddr)
		s.stats.PacketsDropped.Add(1)
		return err
	}
	s.edgeMu.Lock()
	s.edgesByMAC[edge.MACAddr] = edge
	s.edgesBySocket[edge.UDPAddr().String()] = edge
	s.edgeMu.Unlock()

	rresp.IsRegisterOk = true
	rresp.VirtualIP = edge.VirtualIP.String()
	rresp.Masklen = edge.VNetMaskLen
	s.SendStruct(rresp, reg.Msg.CommunityName, s.MacADDR(), nil, r.FromAddr)

	pil := newPeerInfoEvent(p2p.TypeRegister, edge)
	/*peerInfoPayload, err := pil.Encode()
	if err != nil {
		log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
	} else {*/
	return s.BroadcastStruct(pil, cm, s.MacADDR(), nil, reg.EdgeMACAddr())
	//s.BroadcastPacket(spec.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), reg.Msg.EdgeMACAddr)
	//	}

	//return nil
}

func (s *Supernode) handleHeartbeatMessage(r *protocol.RawMessage) error {
	pulse, err := protocol.ToMessage[*netstruct.HeartbeatPulse](r) //r.ToHeartbeatMessage()
	if err != nil {
		return err
	}
	cm, err := s.GetCommunity(pulse)
	if err != nil {
		return err
	}
	s.stats.HeartbeatsReceived.Add(1)
	changed, err := cm.RefreshEdge(pulse)
	if err != nil {
		return err
	}
	edge, exists := cm.GetEdge(pulse.EdgeMACAddr())
	if exists {
		if changed {
			pil := newPeerInfoEvent(p2p.TypeRegister, edge)
			/*peerInfoPayload, err := pil.Encode()
			if err != nil {
				log.Printf("Supernode: (warn) unable to send registration event to peers for community %s: %v", cm.Name(), err)
			} else {*/
			s.BroadcastStruct(pil, cm, s.MacADDR(), nil, pulse.EdgeMACAddr())
			//	s.BroadcastPacket(spec.TypePeerInfo, cm, s.MacADDR(), nil, string(peerInfoPayload), pulse.EdgeMACAddr())
			//}
		}
		//return s.SendAck(r.FromAddr, edge, "ACK")
	}
	return nil
}

func (s *Supernode) handleP2PFullStateMessage(r *protocol.RawMessage) error {
	fsMsg, err := protocol.ToMessage[*p2p.P2PFullState](r)
	if err != nil {
		return err
	}
	if !fsMsg.Msg.IsRequest {
		return fmt.Errorf("supernode does not handle non-requests P2PFullStatesMessage")
	}
	cm, err := s.GetCommunityForEdge(fsMsg.EdgeMACAddr(), fsMsg.CommunityHash())
	if err != nil {
		return err
	}
	P2PFullState, err := cm.GetCommunityPeerP2PInfosDatas(fsMsg.EdgeMACAddr())
	if err != nil {
		return err
	}
	target, err := cm.GetEdgeUDPAddr(fsMsg.EdgeMACAddr())
	if err != nil {
		return err
	}
	return s.SendStruct(P2PFullState, cm.Name(), s.MacADDR(), nil, target)
}

// handlePingMessage should only be forwareded
func (s *Supernode) handlePingMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {
	pingMsg, err := protocol.ToMessage[*netstruct.PeerToPing](r)
	if err != nil {
		return err
	}
	cm, err := s.GetCommunity(pingMsg)
	if err != nil {
		return err
	}
	senderEdge, found := cm.GetEdge(pingMsg.EdgeMACAddr())
	if !found {
		return fmt.Errorf("cannot find senderEdge %v in community for pingMsg packet handling", pingMsg.EdgeMACAddr())
	}
	var targetEdge *Edge
	if pingMsg.DestMACAddr() != "" {
		te, found := cm.GetEdge(pingMsg.DestMACAddr())
		if found {
			targetEdge = te
		}
	}
	if targetEdge == nil {
		return fmt.Errorf("cannot find targetEdge %s in community for pingMsg packet handling", pingMsg.DestMACAddr())
	}
	// Update sender's heartbeat and sequence
	cm.edgeMu.Lock()
	if e, ok := cm.edges[pingMsg.EdgeMACAddr()]; ok {
		e.LastHeartbeat = time.Now()
		e.LastSequence = pingMsg.Header.Sequence
	}
	cm.edgeMu.Unlock()

	s.debugLog("Ping packet received from edge %s", senderEdge.MACAddr)

	if err := s.forwardPacket(pingMsg.ToPacket(), targetEdge); err != nil {
		s.stats.PacketsDropped.Add(1)
		log.Printf("Supernode: Failed to forward pingMessage to edge %s: %v", targetEdge.MACAddr, err)
		return fmt.Errorf("Supernode: Failed to forward pingMessage to edge %s: %v", targetEdge.MACAddr, err)
	}
	s.debugLog("Forwarded packet to edge %s", targetEdge.MACAddr)
	s.stats.PacketsForwarded.Add(1)

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

package supernode

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
)

func (s *Supernode) handleP2PStateInfoMessage(r *protocol.RawMessage) error {
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

func (s *Supernode) handleLeasesInfosMessage(r *protocol.RawMessage) error {
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
	leases := cm.GetLeasesWithEdgesInfos()
	infos := &netstruct.LeasesInfos{
		IsRequest:            false,
		CommunityName:        cm.Name(),
		LeasesWithEdgesInfos: leases,
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
	_, err = s.ValidateEdgeClaimedMACAddr(unReg.EdgeMACAddr(), unReg.Msg.EncryptedMachineID, unReg.Msg.CommunityName)
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

	rresp := &netstruct.RegisterResponse{}
	edge, cm, err := s.RegisterEdge(reg)
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
	return s.BroadcastStruct(pil, cm, s.MacADDR(), nil, reg.EdgeMACAddr())
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
	decmacid, err := s.DecryptMachineID(pulse.Msg.EncryptedMachineID)
	if err != nil {
		return err
	}
	pulse.Msg.ClearMachineID = decmacid
	changed, err := cm.RefreshEdge(pulse)
	if err != nil {
		return err
	}

	edge, exists := cm.GetEdge(pulse.EdgeMACAddr())
	if exists {
		if changed {
			pil := newPeerInfoEvent(p2p.TypeRegister, edge)
			s.BroadcastStruct(pil, cm, s.MacADDR(), nil, pulse.EdgeMACAddr())
		}
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
	if r.Header.PacketType != spec.TypePing {
		return fmt.Errorf("not a %s packet", spec.TypePing)
	}
	return s.ForwardUnicast(r)
}

func (s *Supernode) handleSNPublicSecretMessage(r *protocol.RawMessage) error {
	secretMsg, err := protocol.ToMessage[*netstruct.SNPublicSecret](r)
	if err != nil {
		return err
	}
	if !secretMsg.Msg.IsRequest {
		return fmt.Errorf("supernode handle only snpublicsecret requests")
	}
	resp := &netstruct.SNPublicSecret{
		IsRequest: false,
		PemData:   s.SNSecrets.Pem,
	}
	return s.SendStruct(resp, "", s.MacADDR(), nil, secretMsg.FromAddr)
}

// handleDataMessage processes a data packet
func (s *Supernode) handleDataMessage(r *protocol.RawMessage) error { //packet []byte, srcID, community, destMAC string, seq uint16) {
	if r.Header.PacketType != spec.TypeData {
		return fmt.Errorf("not a %s packet type", spec.TypeData)
	}
	return s.ForwardWithFallBack(r)
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

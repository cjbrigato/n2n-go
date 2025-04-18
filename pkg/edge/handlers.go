package edge

import (
	"errors"
	"fmt"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
)

var ErrNACKRegister = errors.New("Edge: supernode refused register request. Aborting")

func (e *EdgeClient) handleSNPublicSecretMessage(r *protocol.RawMessage) error {
	rresp, err := protocol.ToMessage[*netstruct.SNPublicSecret](r)
	if err != nil {
		return err
	}

	pubkey, err := crypto.PublicKeyFromPEMData(rresp.Msg.PemData)
	if err != nil {
		return err
	}
	e.SNPubKey = pubkey
	log.Printf("Updated Supernode public key !")
	e.isWaitingForSNPubKeyUpdate = false
	if e.isWaitingForSNRetryRegisterResponse {

		err = e.RequestRegister()
		if err != nil {
			return err
		}

	}
	return nil
}

func (e *EdgeClient) handleRegisterResponseMessage(r *protocol.RawMessage) error {
	rresp, err := protocol.ToMessage[*netstruct.RegisterResponse](r)
	if err != nil {
		return err
	}
	if !rresp.Msg.IsRegisterOk {
		return ErrNACKRegister
	}

	log.Printf("Successfull Supernode Reregister")
	e.isWaitingForSNRetryRegisterResponse = false
	e.Peers.IsWaitingCommunityDatas = false
	log.Printf("sending Recovery Peer List Request")
	err = e.sendPeerListRequest()
	if err != nil {
		log.Printf("(warn) failed sending Recovery Peer List Request: %v", err)
	}
	return nil
}

// handleVFuzePacket processes incoming VFuze packets
func (e *EdgeClient) handleVFuzePacket(packetBuf []byte, n int, addr *net.UDPAddr) error {
	if !e.enableVFuze {
		log.Printf("received VFuze data packet from %v but VFuze support is disabled", addr)
		return nil
	}

	dst, err := protocol.VFuzePacketDestMACAddr(packetBuf)
	if err != nil {
		log.Printf("ignoring VFuze packet with non extractable destMac: %v", err)
		return nil
	}

	if e.MACAddr.String() != dst.String() {
		log.Printf("ignoring VFuze packet with destMAC (%s) not matching our (%s)", dst.String(), e.MACAddr.String())
		return nil
	}

	if !disablePeerSocketCheckingInVFuze {
		// if not from supernode, we check that we now this peer
		if !e.IsSupernodeUDPAddr(addr) {
			if !e.IsKnownPeerSocket(addr) {
				log.Printf("ignoring VFuze packet from not in known peers: %s", addr.String())
				return nil
			}
		}
	}

	return e.handleDataPayload(packetBuf[protocol.ProtoVFuzeSize:n])
}

func (e *EdgeClient) handleDataPayload(payload []byte) error {
	payload, err := e.ProcessIncomingPayload(payload)
	if err != nil {
		return fmt.Errorf("error while processing Incoming data packets, droping (err: %w)", err)
	}
	_, err = e.TAP.Write(payload)
	if err != nil {
		return fmt.Errorf("TAP write error: %w", err)
	}
	return nil
}

func (e *EdgeClient) handleDataMessage(r *protocol.RawMessage) error {
	return e.handleDataPayload(r.Payload)
}

func (e *EdgeClient) handlePeerInfoMessage(r *protocol.RawMessage) error {
	peerMsg, err := protocol.ToMessage[*p2p.PeerInfoList](r) //r.ToPeerInfoMessage()
	if err != nil {
		return err
	}
	peerInfos := peerMsg.Msg
	err = e.Peers.HandlePeerInfoList(peerInfos, false, true)
	if err != nil {
		log.Printf("error in HandlePeerInfoList: %v", err)
		return err
	}
	return nil
}

func (s *EdgeClient) handleLeasesInfosMessage(r *protocol.RawMessage) error {
	leaseMsg, err := protocol.ToMessage[*netstruct.LeasesInfos](r)
	if err != nil {
		return err
	}
	if leaseMsg.Msg.IsRequest {
		return fmt.Errorf("edge do not handle request LeasesInfosMessage")
	}
	s.EAPI.LastLeasesInfos = leaseMsg.Msg
	s.EAPI.IsWaitingForLeasesInfos = false
	return nil
}

func (e *EdgeClient) handleRetryRegisterRequest(r *protocol.RawMessage) error {
	if r.Header.PacketType != spec.TypeRetryRegisterRequest {
		return fmt.Errorf(" routing failure: not a TypeRetryRegisterRequest")
	}

	if e.isWaitingForSNRetryRegisterResponse {
		return nil
	}

	log.Printf("Received RetryRegisteRequest from recovering supernode. Trying gracefull Re-regisration...")

	err := e.RequestSNPublicKey()
	if err != nil {
		return err
	}
	e.isWaitingForSNPubKeyUpdate = true
	e.isWaitingForSNRetryRegisterResponse = true

	return nil
}

func (e *EdgeClient) handleP2PFullStateMessage(r *protocol.RawMessage) error {
	fstateMsg, err := protocol.ToMessage[*p2p.P2PFullState](r) //r.ToP2PFullStateMessage()
	if err != nil {
		return err
	}
	if fstateMsg.Msg.IsRequest {
		return fmt.Errorf("edge shall not received Request type P2PFullStateMessage")
	}
	return e.Peers.UpdateP2PCommunityDatas(fstateMsg.Msg.Reachables, fstateMsg.Msg.UnReachables)
}

func (e *EdgeClient) handlePingMessage(r *protocol.RawMessage) error {
	pingMsg, err := protocol.ToMessage[*netstruct.PeerToPing](r)
	if err != nil {
		return err
	}
	// If it is PING message, answer with pong and CheckID payload
	if !pingMsg.Msg.IsPong {
		// swap dst/src
		dst, err := net.ParseMAC(pingMsg.EdgeMACAddr())
		if err != nil {
			return fmt.Errorf("cannot parse dst EdgeMACAddr for swaping")
		}
		if pingMsg.DestMACAddr() != e.MACAddr.String() {
			return fmt.Errorf("ping recipient differs from this edge MACAddress")
		}
		pongMsg := &netstruct.PeerToPing{
			IsPong:  true,
			CheckID: pingMsg.Msg.CheckID,
		}
		e.SendStruct(pongMsg, dst, p2p.UDPBestEffort)
	} else {
		// if it is a PONG message, check OUR last pings and update P2PStates accordingly
		p, err := e.Peers.GetPeer(pingMsg.EdgeMACAddr())
		if err != nil {
			return fmt.Errorf("received a pong for a MACAddress %s not in our peers list", pingMsg.EdgeMACAddr())
		}
		if p.P2PCheckID == pingMsg.Msg.CheckID {
			if p.UpdateP2PStatus(p2p.P2PAvailable, pingMsg.Msg.CheckID) {
				e.Peers.SetPendingChanges()
			}
		} else {
			err = fmt.Errorf("received a pong for MACAddress %s but checkID differs (want %s, received %s)", pingMsg.EdgeMACAddr(), p.P2PCheckID, pingMsg.Msg.CheckID)
			if p.UpdateP2PStatus(p2p.P2PUnknown, "") {
				e.Peers.SetPendingChanges()
			}
		}
		if p.P2PStatus == p2p.P2PAvailable {
			if !pingMsg.Header.IsFromSupernode() {
				if changed, _ := p.SetFullDuplex(true); changed {
					e.Peers.SetPendingChanges()
				}
			} else {
				if changed, _ := p.SetFullDuplex(false); changed {
					e.Peers.SetPendingChanges()
				}
			}
		}
	}
	return nil
}

package edge

import (
	"errors"
	"fmt"
	"log"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"strings"
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
	log.Printf("Edge: Updated Supernode public key !")
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

	log.Printf("Edge: Successfull Supernode Reregister")
	e.isWaitingForSNRetryRegisterResponse = false
	e.Peers.IsWaitingCommunityDatas = false
	log.Printf("Edge: sending Recovery Peer List Request")
	err = e.sendPeerListRequest()
	if err != nil {
		log.Printf("Edge: (warn) failed sending Recovery Peer List Request: %v", err)
	}
	return nil
}

func (e *EdgeClient) handleDataMessage(r *protocol.RawMessage) error {
	payload := r.Payload
	if e.encryptionEnabled {
		plainPayload, err := crypto.DecryptPayload(e.EncryptionKey, payload)
		if err != nil {
			return fmt.Errorf("error while decrypting data packets, droping (err: %w)", err)
		}
		payload = plainPayload
	}
	_, err := e.TAP.Write(payload)
	if err != nil {
		if !strings.Contains(err.Error(), "file already closed") {
			log.Printf("Edge: TAP write error: %v", err)
			return err
		}
	}
	return nil
}

func (e *EdgeClient) handlePeerInfoMessage(r *protocol.RawMessage) error {
	peerMsg, err := protocol.ToMessage[*p2p.PeerInfoList](r) //r.ToPeerInfoMessage()
	if err != nil {
		return err
	}
	peerInfos := peerMsg.Msg
	err = e.Peers.HandlePeerInfoList(peerInfos, false, true)
	if err != nil {
		log.Printf("Edge: error in HandlePeerInfoList: %v", err)
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
		return fmt.Errorf("Edge do not handle request LeasesInfosMessage")
	}
	s.EAPI.LastLeasesInfos = leaseMsg.Msg
	s.EAPI.IsWaitingForLeasesInfos = false
	return nil
}

func (e *EdgeClient) handleRetryRegisterRequest(r *protocol.RawMessage) error {
	if r.Header.PacketType != spec.TypeRetryRegisterRequest {
		return fmt.Errorf("Edge: routing failure: not a TypeRetryRegisterRequest")
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
			p.UpdateP2PStatus(p2p.P2PAvailable, pingMsg.Msg.CheckID)
		} else {
			err = fmt.Errorf("received a pong for MACAddress %s but checkID differs (want %s, received %s)", pingMsg.EdgeMACAddr(), p.P2PCheckID, pingMsg.Msg.CheckID)
			p.UpdateP2PStatus(p2p.P2PUnknown, "")
		}
		if p.P2PStatus == p2p.P2PAvailable {
			if !pingMsg.Header.IsFromSupernode() {
				p.SetFullDuplex(true)
			} else {
				p.SetFullDuplex(false)
			}
		}
	}
	return nil
}

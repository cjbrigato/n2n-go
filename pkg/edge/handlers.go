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
	"time"
)

var ErrNACKRegister = errors.New("Edge: supernode refused register request. Aborting")

func (e *EdgeClient) RequestSNPublicKey() error {
	log.Printf("Trying to get Supernode publickey with supernode at %s...", e.SupernodeAddr)

	reqPub := &netstruct.SNPublicSecret{
		IsRequest: true,
	}

	return e.SendStruct(reqPub, nil, p2p.UDPEnforceSupernode)
}

func (e *EdgeClient) RequestRegister() error {
	log.Printf("Registering with supernode at %s...", e.SupernodeAddr)

	encMachineID, err := e.EncryptedMachineID()
	if err != nil {
		return err
	}

	regReq := &netstruct.RegisterRequest{
		EdgeMACAddr:        e.MACAddr.String(),
		EdgeDesc:           e.ID,
		CommunityName:      e.Community,
		EncryptedMachineID: encMachineID,
	}

	return e.SendStruct(regReq, nil, p2p.UDPEnforceSupernode)
}

// Register sends a registration packet to the supernode.
func (e *EdgeClient) GetSNPublicKey() error {
	err := e.RequestSNPublicKey()
	if err != nil {
		return err
	}
	// Set a timeout for the response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Read the response
	respBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(respBuf)

	n, addr, err := e.Conn.ReadFromUDP(respBuf)
	if err != nil {
		return fmt.Errorf("edge: pubkey ACK timeout: %w", err)
	}

	// Reset deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	if n < protocol.ProtoVHeaderSize {
		return fmt.Errorf("Edge: short packet while waiting for initial SnSecretsPub")
	}

	rresp, err := protocol.MessageFromPacket[*netstruct.SNPublicSecret](respBuf, addr)
	if err != nil {
		return err
	}

	pubkey, err := crypto.PublicKeyFromPEMData(rresp.Msg.PemData)
	if err != nil {
		return err
	}
	e.SNPubKey = pubkey
	log.Printf("Got Supernode public key !")

	return nil
}

func (e *EdgeClient) EncryptedMachineID() ([]byte, error) {
	encMachineID, err := crypto.EncryptSequence(e.machineId, e.SNPubKey)
	if err != nil {
		return nil, err
	}
	return encMachineID, nil
}

// Register sends a registration packet to the supernode.
func (e *EdgeClient) Register() error {
	err := e.RequestRegister()
	if err != nil {
		return err
	}

	// Set a timeout for the response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Read the response
	respBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(respBuf)

	n, addr, err := e.Conn.ReadFromUDP(respBuf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %w", err)
	}

	// Reset deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	if n < protocol.ProtoVHeaderSize {
		return fmt.Errorf("Edge: short packet while waiting for initial RegisterResponse")
	}

	rresp, err := protocol.MessageFromPacket[*netstruct.RegisterResponse](respBuf, addr)

	if err != nil {
		return err
	}

	if !rresp.Msg.IsRegisterOk {
		return ErrNACKRegister
	}
	e.VirtualIP = fmt.Sprintf("%s/%d", rresp.Msg.VirtualIP, rresp.Msg.Masklen)
	log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP)
	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	e.registered = true
	return nil
}

// Unregister sends an unregister packet to the supernode.
func (e *EdgeClient) Unregister() error {
	if !e.registered {
		return fmt.Errorf("cannot unregister an unregistered edge")
	}
	encMacid, err := e.EncryptedMachineID()
	if err != nil {
		return err
	}
	var unregErr error
	e.unregisterOnce.Do(func() {
		unreg := &netstruct.UnregisterRequest{
			EdgeMACAddr:        e.MACAddr.String(),
			CommunityName:      e.Community,
			EncryptedMachineID: encMacid,
		}
		err := e.SendStruct(unreg, nil, p2p.UDPEnforceSupernode)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

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

	err := e.RequestSNPublicKey()
	if err != nil {
		return err
	}
	e.isWaitingForSNPubKeyUpdate = true

	for {
		if e.isWaitingForSNPubKeyUpdate {
			time.Sleep(300 * time.Millisecond)
		} else {
			break
		}
	}

	err = e.RequestRegister()
	if err != nil {
		return err
	}

	for {
		if e.isWaitingForSNRetryRegisterResponse {
			time.Sleep(300 * time.Millisecond)
		} else {
			break
		}
	}

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
	if fstateMsg.Msg.FullState == nil {
		return fmt.Errorf("received nil FullState in P2PFullStateMessage")
	}
	e.Peers.FullState = fstateMsg.Msg.FullState
	if e.Peers.IsWaitingForFullState {
		e.Peers.IsWaitingForFullState = false
	}
	return nil
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
	}
	return nil
}

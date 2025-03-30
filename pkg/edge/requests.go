package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol/netstruct"
)

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
			unregErr = fmt.Errorf(" failed to send unregister: %w", err)
			return
		}
		log.Printf("Unregister message sent")
	})
	return unregErr
}

// sendPeerListRequest sends a PeerRequest for all but sender's peerinfos
// scoped by community
func (e *EdgeClient) sendPeerListRequest() error {
	req := &netstruct.PeerListRequest{
		CommunityName: e.Community,
	}
	err := e.SendStruct(req, nil, p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf(" failed to send peerList Request: %w", err)
	}
	return nil
}

// sendHeartbeat sends a single heartbeat message
func (e *EdgeClient) sendHeartbeat() error {
	//seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	if e.isWaitingForSNPubKeyUpdate || e.isWaitingForSNRetryRegisterResponse {
		return fmt.Errorf(" not sending heartbing while waiting for SNPubkeyUpdate or SNRetryRegisterResponse")
	}

	encmacid, err := e.EncryptedMachineID()
	if err != nil {
		return fmt.Errorf(" failed to send heartbeat: %w", err)
	}
	pulse := &netstruct.HeartbeatPulse{EdgeMACAddr: e.MACAddr.String(), CommunityName: e.Community, EncryptedMachineID: encmacid}
	err = e.SendStruct(pulse, nil, p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf(" failed to send heartbeat: %w", err)
	}
	return nil
}

func (e *EdgeClient) sendP2PInfos() error {

	if e.isWaitingForSNPubKeyUpdate || e.isWaitingForSNRetryRegisterResponse {
		return fmt.Errorf(" not sending P2PInfos while waiting for SNPubkeyUpdate or SNRetryRegisterResponse")
	}

	if !e.Peers.HasPendingChanges() {
		return nil
	}

	log.Printf("sending pending PeerP2PInfos changes to supernode...")

	infos := e.Peers.GetPeerP2PInfos()
	e.Peers.ClearPendingChanges()
	err := e.SendStruct(infos, nil, p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf(" failed to send updated P2PInfos: %w", err)
	}
	return nil
}

func (e *EdgeClient) sendP2PFullStateRequest() error {
	if e.Peers.IsWaitingCommunityDatas {
		return nil
	}
	e.Peers.IsWaitingCommunityDatas = true
	req := &p2p.P2PFullState{
		CommunityName: e.Community,
		IsRequest:     true,
		P2PCommunityDatas: p2p.P2PCommunityDatas{
			Reachables:   make(map[string]p2p.PeerP2PInfos),
			UnReachables: make(map[string]p2p.PeerCachedInfo),
		},
	}
	err := e.SendStruct(req, nil, p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf(" failed to send updated P2PInfos: %w", err)
	}

	return nil
}

func (eapi *EdgeClientApi) sendLeasesInfosRequest() error {
	if eapi.IsWaitingForLeasesInfos {
		return nil
	}
	req := &netstruct.LeasesInfos{
		CommunityName: eapi.Client.Community,
		IsRequest:     true,
	}
	err := eapi.Client.SendStruct(req, nil, p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf(" failed to send updated P2PInfos: %w", err)
	}
	eapi.IsWaitingForLeasesInfos = true
	return nil
}

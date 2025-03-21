package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"net"
	"strings"
	"time"
)

// Register sends a registration packet to the supernode.
// Registration payload format depends on the header type:
// - Legacy: "REGISTER <edgeID> <tapMAC>" (MAC in hex colon-separated form)
// - Compact: "REGISTER <edgeID> <tapMAC> <community> <communityHash>" when extended addressing is used
// - ProtoV: "REGISTER <edgeDesc> <CommunityName>"
func (e *EdgeClient) Register() error {
	log.Printf("Registering with supernode at %s...", e.SupernodeAddr)

	payloadStr := fmt.Sprintf("REGISTER %s %s ",
		e.ID, e.Community)
	err := e.WritePacket(protocol.TypeRegisterRequest, e.MACAddr, payloadStr, p2p.UDPEnforceSupernode)
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

	// Process the response
	resp := strings.TrimSpace(string(respBuf[:n]))
	parts := strings.Fields(resp)
	if len(parts) < 1 || parts[0] != "ACK" {
		if strings.HasPrefix(resp, "ERR") {
			return fmt.Errorf("edge: registration error: %s", resp)
		}
		return fmt.Errorf("edge: unexpected registration response from %v: %s", addr, resp)
	}

	if len(parts) >= 3 {
		e.VirtualIP = fmt.Sprintf("%s/%s", parts[1], parts[2])
		log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP)
	} else {
		return fmt.Errorf("edge: registration response missing virtual IP")
	}

	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	e.registered = true
	return nil
}

// Unregister sends an unregister packet to the supernode.
func (e *EdgeClient) Unregister() error {
	if !e.registered {
		return fmt.Errorf("cannot unregister an unregistered edge")
	}
	var unregErr error
	e.unregisterOnce.Do(func() {
		payloadStr := fmt.Sprintf("UNREGISTER %s ", e.ID)
		err := e.WritePacket(protocol.TypeUnregisterRequest, nil, payloadStr, p2p.UDPEnforceSupernode)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %w", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

func (e *EdgeClient) handleDataMessage(r *protocol.RawMessage) error {
	_, err := e.TAP.Write(r.Payload)
	if err != nil {
		if !strings.Contains(err.Error(), "file already closed") {
			log.Printf("Edge: TAP write error: %v", err)
			return err
		}
	}
	return nil
}

func (e *EdgeClient) handlePeerInfoMessage(r *protocol.RawMessage) error {
	peerMsg, err := r.ToPeerInfoMessage()
	if err != nil {
		return err
	}
	peerInfos := peerMsg.PeerInfoList
	err = e.Peers.HandlePeerInfoList(peerInfos, false, true)
	if err != nil {
		log.Printf("Edge: error in HandlePeerInfoList: %v", err)
		return err
	}
	return nil
}

func (e *EdgeClient) handleP2PFullStateMessage(r *protocol.RawMessage) error {
	fstateMsg, err := r.ToP2PFullStateMessage()
	if err != nil {
		return err
	}
	if fstateMsg.IsRequest {
		return fmt.Errorf("edge shall not received Request type P2PFullStateMessage")
	}
	if fstateMsg.P2PFullState.FullState == nil {
		return fmt.Errorf("received nil FullState in P2PFullStateMessage")
	}
	e.Peers.FullState = fstateMsg.P2PFullState.FullState
	if e.Peers.IsWaitingForFullState {
		e.Peers.IsWaitingForFullState = false
	}
	//log.Printf("Edge: updated registry P2PFullState")
	return nil
}

func (e *EdgeClient) handlePingMessage(r *protocol.RawMessage) error {
	pingMsg, err := r.ToPingMessage()
	if err != nil {
		return err
	}
	if !pingMsg.IsPong {
		// swap dst/src
		dst, err := net.ParseMAC(pingMsg.EdgeMACAddr)
		if err != nil {
			return fmt.Errorf("cannot parse dst EdgeMACAddr for swaping")
		}
		if pingMsg.DestMACAddr != e.MACAddr.String() {
			return fmt.Errorf("ping recipient differs from this edge MACAddress")
		}
		payloadStr := fmt.Sprintf("PONG %s ", pingMsg.CheckID)
		e.WritePacket(protocol.TypePing, dst, payloadStr, p2p.UDPBestEffort)
	} else {
		p, err := e.Peers.GetPeer(pingMsg.EdgeMACAddr)
		if err != nil {
			return fmt.Errorf("received a pong for a MACAddress %s not in our peers list", pingMsg.EdgeMACAddr)
		}
		if p.P2PCheckID == pingMsg.CheckID {
			p.UpdateP2PStatus(p2p.P2PAvailable, pingMsg.CheckID)
		} else {
			err = fmt.Errorf("received a pong for MACAddress %s but checkID differs (want %s, received %s)", pingMsg.EdgeMACAddr, p.P2PCheckID, pingMsg.CheckID)
			p.UpdateP2PStatus(p2p.P2PUnknown, "")
		}
	}
	return nil
}

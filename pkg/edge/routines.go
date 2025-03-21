package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

func (e *EdgeClient) UpdatePeersP2PStates() {
	peers := e.Peers.GetP2PUnknownPeers()
	for _, p := range peers {
		err := e.PingPeer(p, 5, 300*time.Second, p2p.P2PPending)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
	peers = e.Peers.GetP2PendingPeers()
	for _, p := range peers {
		err := e.PingPeer(p, 5, 300*time.Second, p2p.P2PPending)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
	peers = e.Peers.GetP2PAvailablePeers()
	for _, p := range peers {
		err := e.PingPeer(p, 5, 300*time.Millisecond, p2p.P2PAvailable)
		if err != nil {
			log.Printf("handleP2PUpdates: error in UpdatePeersP2PStates for peer with MACAddress %s: %v", p.Infos.MACAddr.String(), err)
		}
	}
}

func (e *EdgeClient) PingPeer(p *p2p.Peer, n int, interval time.Duration, status p2p.P2PCapacity) error {
	checkid := fmt.Sprintf("%s.%s.%s.%s.%d", e.ID, e.MACAddr.String(), p.Infos.MACAddr.String(), p.Infos.PubSocket.IP.String(), p.Infos.PubSocket.Port)
	payloadStr := fmt.Sprintf("PING %s ", checkid)
	p.UpdateP2PStatus(status, checkid)
	for range n {
		e.WritePacket(protocol.TypePing, p.Infos.MACAddr, payloadStr, p2p.UDPEnforceP2P)
	}
	return e.WritePacket(protocol.TypePing, p.Infos.MACAddr, payloadStr, p2p.UDPEnforceP2P)
}

// handleHeartbeat sends heartbeat messages periodically
func (e *EdgeClient) handleP2PUpdates() {
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.UpdatePeersP2PStates()
		case <-e.ctx.Done():
			return
		}
	}
}

// Run launches heartbeat, TAP-to-supernode, and UDP-to-TAP goroutines.
func (e *EdgeClient) Run() {
	if !e.running.CompareAndSwap(false, true) {
		log.Printf("Edge: Already running, ignoring Run() call")
		return
	}
	if !e.registered {
		log.Printf("Edge: Cannot run an unregistered edge, ignoring Run() call")
	}

	go e.handleHeartbeat()
	go e.handleTAP()
	go e.handleUDP()

	log.Printf("Edge: sending preliminary Peer Request")
	err := e.sendPeerRequest()
	if err != nil {
		log.Printf("Edge: (warn) failed sending preliminary Peer Request: %v", err)
	}

	log.Printf("Edge: starting P2PUpdate routines...")
	go e.handleP2PUpdates()
	go e.handleP2PInfos()

	log.Printf("Edge: starting management api...")
	eapi := NewEdgeApi(e)
	e.EAPI = eapi
	go eapi.Run()

	<-e.ctx.Done() // Block until context is cancelled
}

// Close initiates a clean shutdown.
func (e *EdgeClient) Close() {
	if err := e.Unregister(); err != nil {
		log.Printf("Edge: Unregister failed: %v", err)
	}
	if e.IgdClient != nil {
		log.Printf(" UPnP > Cleaning up all portMappings...")
		e.IgdClient.CleanupAllMappings()
	}
	e.cancel()

	// Force read operations to unblock
	if e.Conn != nil {
		e.Conn.SetReadDeadline(time.Now())
	}

	// Wait for all goroutines to finish
	e.wg.Wait()

	// Close resources
	if e.TAP != nil {
		if err := e.TAP.Close(); err != nil {
			log.Printf("Edge: Error closing TAP interface: %v", err)
		}
	}

	if e.Conn != nil {
		if err := e.Conn.Close(); err != nil {
			log.Printf("Edge: Error closing UDP connection: %v", err)
		}
	}

	e.running.Store(false)
	log.Printf("Edge: Shutdown complete")
}

// sendPeerRequest sends a PeerRequest for all but sender's peerinfos
// scoped by community
func (e *EdgeClient) sendPeerRequest() error {
	//seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	err := e.WritePacket(protocol.TypePeerRequest, nil, fmt.Sprintf("PEERREQUEST %s ", e.Community), p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send peerRequest: %w", err)
	}
	return nil
}

// sendHeartbeat sends a single heartbeat message
func (e *EdgeClient) sendHeartbeat() error {
	//seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

	err := e.WritePacket(protocol.TypeHeartbeat, nil, fmt.Sprintf("HEARTBEAT %s ", e.Community), p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send heartbeat: %w", err)
	}
	return nil
}

func (e *EdgeClient) sendP2PInfos() error {
	infos := e.Peers.GetPeerP2PInfos()
	data, err := infos.Encode()
	if err != nil {
		return err
	}
	err = e.WritePacket(protocol.TypeP2PStateInfo, nil, string(data), p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send updated P2PInfos: %w", err)
	}
	return nil
}

func (e *EdgeClient) handleP2PInfos() {
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := e.sendP2PInfos(); err != nil {
				log.Printf("Edge: sendP2PInfos error: %v", err)
			}
		case <-e.ctx.Done():
			return
		}
	}
}

// handleHeartbeat sends heartbeat messages periodically
func (e *EdgeClient) handleHeartbeat() {
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := e.sendHeartbeat(); err != nil {
				log.Printf("Edge: Heartbeat error: %v", err)
			}
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *EdgeClient) handleTAPVFuze(destMAC net.HardwareAddr, n int, payloadBuf []byte, udpSocket *net.UDPAddr) error {
	vfuzh := protocol.VFuzeHeaderBytes(destMAC)
	totalLen := protocol.ProtoVFuzeSize + n
	packet := make([]byte, totalLen)
	copy(packet[0:7], vfuzh[0:7])
	copy(packet[7:], payloadBuf[:n])
	e.PacketsSent.Add(1)
	_, err := e.Conn.WriteToUDP(packet[:totalLen], udpSocket)
	if err != nil {
		return err
	}
	return nil
}

// handleTAP reads packets from the TAP interface and (potentially) sends them to the supernode.
func (e *EdgeClient) handleTAP() {
	e.wg.Add(1)
	defer e.wg.Done()

	// Preallocate the buffer once - no need to reallocate for each packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	// Create separate areas for header and payload
	headerSize := protocol.ProtoVHeaderSize

	headerBuf := packetBuf[:headerSize]
	payloadBuf := packetBuf[headerSize:]

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue processing
		}

		// Read directly into payload area to avoid a copy
		n, err := e.TAP.Read(payloadBuf)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP read error: %v", err)
			continue
		}

		if n < 14 {
			log.Printf("Edge: Packet too short to contain Ethernet header (%d bytes)", n)
			continue
		}

		ethertype, err := tuntap.GetEthertype(payloadBuf)
		if err != nil {
			log.Printf("Edge: Cannot parse link layer frame for Ethertype, skipping: %v", err)
			continue
		}
		if ethertype == tuntap.IPv6 {
			//log.Printf("Edge: (warn) skipping TAP frame with IPv6 Ethertype: %v", ethertype)
			continue
		}
		udpSocket := e.SupernodeAddr
		destMAC := tuntap.FastDestination(payloadBuf)
		if !tuntap.IsBroadcast(destMAC) {
			udpSocket, err = e.UDPAddrWithStrategy(destMAC, p2p.UDPBestEffort)
			if err != nil {
				log.Printf("Edge: Error getting udpSocket with Strategy in handleTAP with destMAC: %v", err)
				continue
			}
			if e.enableVFuze {
				err = e.handleTAPVFuze(destMAC, n, payloadBuf, udpSocket)
				if err != nil {
					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					log.Printf("Edge: Error sending packet with enableVFuze from TAP: %v with socket: %s", err, udpSocket)
				}
				continue
			}
		}

		// Create and marshal header
		seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)

		// Create compact header with destination MAC
		header, err := protocol.NewProtoVHeader(
			e.ProtocolVersion(),
			64,
			protocol.TypeData,
			seq,
			e.Community,
			e.MACAddr,
			destMAC,
		)

		if err := header.MarshalBinaryTo(headerBuf); err != nil {
			log.Printf("Edge: Failed to marshal protov data header: %v", err)
			continue
		}

		// Update stats
		e.PacketsSent.Add(1)

		// Send packet (header is already at the beginning of packetBuf)
		totalLen := headerSize + n
		_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

func (e *EdgeClient) IsSupernodeUDPAddr(addr *net.UDPAddr) bool {
	return (addr.IP.Equal(e.SupernodeAddr.IP)) && (addr.Port == e.SupernodeAddr.Port)
}

/*
func (e *EdgeClient) handleVFrag(data []byte, addr *net.UDPAddr) {
	if len(data) < protocol.ProtoVFragSize {
		log.Println("Received datagram smaller than header size")
		return
	}
	var header protocol.ProtoVFragHeader
	headerReader := bytes.NewReader(data[:protocol.ProtoVFragSize])
	if err := binary.Read(headerReader, binary.BigEndian, &header); err != nil {
		log.Printf("Error parsing header: %v", err)
		return
	}
	log.Printf("%v", header)
	payload := data[protocol.ProtoVFragSize:]
	log.Printf("len p: %d", len(payload))
	// Check if this is a fragmented message
	if header.FragmentTotal <= 1 {
		if header.PacketType == protocol.TypeP2PFullState {
			pil, err := p2p.ParseP2PFullState(payload)
			if err != nil {
				log.Printf("error handleVFrag: %v", err)
				return
			}
			if pil.IsRequest {
				return //fmt.Errorf("edge shall not received Request type P2PFullStateMessage")
			}
			if pil.FullState == nil {
				return //fmt.Errorf("received nil FullState in P2PFullStateMessage")
			}
			e.Peers.FullState = pil.FullState
			if e.Peers.IsWaitingForFullState {
				e.Peers.IsWaitingForFullState = false
			}
			log.Println("mamamia")
			return
		}
	}

}
*/

// handleUDP reads packets from the UDP connection and writes the payload to the TAP interface.
func (e *EdgeClient) handleUDP() {
	e.wg.Add(1)
	defer e.wg.Done()

	// Preallocate buffer for receiving packets
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue processing
		}
		n, addr, err := e.Conn.ReadFromUDP(packetBuf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("Edge: UDP read error: %v", err)
			continue
		}

		/*
			if packetBuf[0] == protocol.VersionVFrag {
				e.handleVFrag(packetBuf[:n], addr)
				continue
			}
		*/

		if e.enableVFuze {
			if packetBuf[0] == protocol.VersionVFuze {
				payload := packetBuf[protocol.ProtoVFuzeSize:n]
				_, err = e.TAP.Write(payload)
				if err != nil {
					if strings.Contains(err.Error(), "file already closed") {
						return
					}
					log.Printf("Edge: TAP write error: %v", err)
				}
				continue
			}
		}

		// Handle short packets and ACKs
		if n < protocol.ProtoVHeaderSize { // Even compact headers have minimum size
			msg := strings.TrimSpace(string(packetBuf[:n]))
			if msg == "ACK" {
				// Just an ACK - nothing to do
				continue
			}
			log.Printf("Edge: Received packet too short from %v: %q", addr, msg)
			continue
		}

		e.PacketsRecv.Add(1)

		rawMsg, err := protocol.NewRawMessage(packetBuf, addr)
		if err != nil {
			log.Printf("Edge: error while parsing UDP Packet: %v", err)
			continue
		}

		handler, exists := e.messageHandlers[rawMsg.Header.PacketType]
		if !exists {
			log.Printf("Edge: Unknown packet type %d from %v", rawMsg.Header.PacketType, rawMsg.Addr)
			continue
		}
		err = handler(rawMsg)
		if err != nil {
			log.Printf("Edge: Error from messageHandler: %v", err)
		}
	}
}

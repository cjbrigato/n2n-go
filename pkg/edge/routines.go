package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"n2n-go/pkg/tuntap"
	"net"
	"strings"
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
	pingMsg := &netstruct.PeerToPing{
		IsPong:  false,
		CheckID: checkid,
	}
	p.UpdateP2PStatus(status, checkid)
	for range n {
		e.SendStruct(pingMsg, p.Infos.MACAddr, p2p.UDPEnforceP2P)
	}
	return e.SendStruct(pingMsg, p.Infos.MACAddr, p2p.UDPEnforceP2P)
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

// handleTAP reads packets from the TAP interface and (potentially) sends them to the supernode.
// What's read from TAP right now are EthernetFrames and thus transformed into DATA packets
// Then sent through UDP, to either Supernode or P2PDirect connection if available and relevant.
// Alternative to Data Packet, if vFuze is enabled, it can be transfered early as Vfuze data packet.
func (e *EdgeClient) handleTAP() {
	e.wg.Add(1)
	defer e.wg.Done()

	// Preallocate the buffer once - no need to reallocate for each packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)

	// Create separate areas for header and payload
	headerSize := protocol.ProtoVHeaderSize
	frameBuf := packetBuf[headerSize:]

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			// Continue processing
		}

		// Read directly into payload area to avoid a copy
		n, err := e.TAP.Read(frameBuf)
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

		ethertype, err := tuntap.GetEthertype(frameBuf)
		if err != nil {
			log.Printf("Edge: Cannot parse link layer frame for Ethertype, skipping: %v", err)
			continue
		}
		if ethertype == tuntap.IPv6 {
			//log.Printf("Edge: (warn) skipping TAP frame with IPv6 Ethertype: %v", ethertype)
			continue
		}

		payload, err := e.MaybeEncrypt(frameBuf[:n])
		if err != nil {
			log.Printf("Edge: Failed to encrypt payload %v", err)
			continue
		}

		strategy := p2p.UDPEnforceSupernode

		destMAC := tuntap.FastDestination(frameBuf)
		// if destMAC is not unicat
		// 1. We change the strategy to BestEffort so we may try P2P Direct connection
		// 2. We switch to vFuze packets if enabled by config
		if !tuntap.IsBroadcast(destMAC) {
			strategy = p2p.UDPBestEffort
			if e.enableVFuze {
				err = e.SendVFuze(destMAC, n, payload, strategy)
				if err != nil {
					if strings.Contains(err.Error(), "use of closed network connection") {
						return
					}
					log.Printf("Edge: Error sending packet with enableVFuze from TAP: %v", err)
				}
				continue
			}
		}

		err = e.WritePacket(spec.TypeData, destMAC, payload, strategy)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

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

		e.PacketsRecv.Add(1)

		if packetBuf[0] == protocol.VersionVFuze {
			if e.enableVFuze {
				err := e.handleDataPayload(packetBuf[protocol.ProtoVFuzeSize:n])
				if err != nil {
					if strings.Contains(err.Error(), "file already closed") {
						return
					}
					log.Printf("Edge: handleDataPayload Error: %v", err)
				}
				continue
			}
			log.Printf("Edge: received VFuze data packet from %v but VFuze support is disabled", addr)
			continue
		}

		if n < protocol.ProtoVHeaderSize {
			log.Printf("Edge: Received packet too short from %v: %q", addr, string(packetBuf[:n]))
			continue
		}

		rawMsg, err := protocol.NewRawMessage(packetBuf[:n], addr)
		if err != nil {
			log.Printf("Edge: error while parsing UDP Packet: %v", err)
			continue
		}

		handler, exists := e.messageHandlers[rawMsg.Header.PacketType]
		if !exists {
			log.Printf("Edge: Unknown packet type %d from %v", rawMsg.Header.PacketType, rawMsg.FromAddr)
			continue
		}
		err = handler(rawMsg)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: Error from messageHandler: %v", err)
		}
	}
}

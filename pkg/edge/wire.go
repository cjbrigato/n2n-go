package edge

import (
	"fmt"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"sync/atomic"
	"time"
)

func (e *EdgeClient) UDPAddrWithStrategy(dst net.HardwareAddr, strategy p2p.UDPWriteStrategy) (*net.UDPAddr, error) {
	var udpSocket *net.UDPAddr
	switch strategy {
	case p2p.UDPEnforceSupernode:
		udpSocket = e.SupernodeAddr
	case p2p.UDPBestEffort, p2p.UDPEnforceP2P:
		if dst == nil {
			if strategy == p2p.UDPEnforceP2P {
				return nil, fmt.Errorf("cannot write packet with UDPEnforceP2P flag and a nil destMACAddr")
			}
			udpSocket = e.SupernodeAddr
			break
		}
		p, err := e.Peers.GetPeer(dst.String())
		if err != nil {
			if strategy == p2p.UDPEnforceP2P {
				return nil, fmt.Errorf("cannot write packet with UDPEnforceP2P (peer not found) %v", err)
			}
			udpSocket = e.SupernodeAddr
			break
		}
		if p.P2PStatus != p2p.P2PAvailable && strategy == p2p.UDPBestEffort {
			udpSocket = e.SupernodeAddr
			break
		}
		udpSocket = p.UDPAddr()
	}
	return udpSocket, nil
}

func (e *EdgeClient) EdgeHeader(pt spec.PacketType, dst net.HardwareAddr) *protocol.ProtoVHeader {
	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	h := &protocol.ProtoVHeader{
		Version:     protocol.VersionV,
		TTL:         64,
		PacketType:  pt,
		Flags:       0,
		Sequence:    seq,
		CommunityID: protocol.HashCommunity(e.Community),
		Timestamp:   uint32(time.Now().Unix()),
	}
	copy(h.SourceID[:], e.MACAddr[:6])
	if dst != nil {
		copy(h.DestID[:], dst[:6])
	}
	return h
}

func (e *EdgeClient) WritePacket(pt spec.PacketType, dst net.HardwareAddr, payload []byte, strategy p2p.UDPWriteStrategy) error {
	udpSocket, err := e.UDPAddrWithStrategy(dst, strategy)
	if err != nil {
		return err
	}

	header := e.EdgeHeader(pt, dst)
	e.PacketsSent.Add(1)

	_, err = e.Conn.WriteToUDP(protocol.PackProtoVDatagram(header, payload), udpSocket)
	if err != nil {
		return fmt.Errorf("edge: failed to send packet: %w", err)
	}
	return nil
}

func (e *EdgeClient) SendStruct(s netstruct.PacketTyped, dst net.HardwareAddr, strategy p2p.UDPWriteStrategy) error {
	udpSocket, err := e.UDPAddrWithStrategy(dst, strategy)
	if err != nil {
		return err
	}

	header := e.EdgeHeader(s.PacketType(), dst)

	/*seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		e.ProtocolVersion(),
		64,
		s.PacketType(),
		seq,
		e.Community,
		e.MACAddr,
		dst,
	)
	if err != nil {
		return err
	}*/

	payload, err := protocol.Encode(s)
	if err != nil {
		return err
	}

	/*if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to protov %s header: %w", s.PacketType(), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], payload)
	totalLen = protocol.ProtoVHeaderSize + payloadLen
	e.PacketsSent.Add(1)

	// Send the packet
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)*/
	_, err = e.Conn.WriteToUDP(protocol.PackProtoVDatagram(header, payload), udpSocket)
	if err != nil {
		return fmt.Errorf("edge: failed to send packet: %w", err)
	}
	return nil
}

// ProtocolVersion returns the protocol version being used
func (e *EdgeClient) ProtocolVersion() uint8 {
	return protocol.VersionV
}

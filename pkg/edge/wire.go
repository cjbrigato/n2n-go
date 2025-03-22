package edge

import (
	"fmt"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"net"
	"sync/atomic"
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

func (e *EdgeClient) WritePacket(pt protocol.PacketType, dst net.HardwareAddr, payloadStr string, strategy p2p.UDPWriteStrategy) error {
	udpSocket, err := e.UDPAddrWithStrategy(dst, strategy)
	if err != nil {
		return err
	}

	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		e.ProtocolVersion(),
		64,
		pt,
		seq,
		e.Community,
		e.MACAddr,
		dst,
	)
	if err != nil {
		return err
	}

	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to protov %s header: %w", pt.String(), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], []byte(payloadStr))
	totalLen = protocol.ProtoVHeaderSize + payloadLen
	e.PacketsSent.Add(1)

	// Send the packet
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)
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

	seq := uint16(atomic.AddUint32(&e.seq, 1) & 0xFFFF)
	// Get buffer for full packet
	packetBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(packetBuf)
	var totalLen int

	header, err := protocol.NewProtoVHeader(
		e.ProtocolVersion(),
		64,
		protocol.PacketType(s.PacketType()),
		seq,
		e.Community,
		e.MACAddr,
		dst,
	)
	if err != nil {
		return err
	}

	payload, err := protocol.Encode(s)
	if err != nil {
		return err
	}

	if err := header.MarshalBinaryTo(packetBuf[:protocol.ProtoVHeaderSize]); err != nil {
		return fmt.Errorf("edge: failed to protov %s header: %w", protocol.PacketType(s.PacketType()), err)
	}
	payloadLen := copy(packetBuf[protocol.ProtoVHeaderSize:], payload)
	totalLen = protocol.ProtoVHeaderSize + payloadLen
	e.PacketsSent.Add(1)

	// Send the packet
	_, err = e.Conn.WriteToUDP(packetBuf[:totalLen], udpSocket)
	if err != nil {
		return fmt.Errorf("edge: failed to send packet: %w", err)
	}
	return nil
}

// ProtocolVersion returns the protocol version being used
func (e *EdgeClient) ProtocolVersion() uint8 {
	return protocol.VersionV
}

// GetHeaderSize returns the current header size in bytes
func (e *EdgeClient) GetHeaderSize() int {
	return protocol.ProtoVHeaderSize
}

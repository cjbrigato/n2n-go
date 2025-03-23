package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"n2n-go/pkg/protocol/codec"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"strings"
)

type RawMessage struct {
	Header    *ProtoVHeader
	Payload   []byte
	FromAddr  *net.UDPAddr
	rawPacket []byte
}

func packProtoVDatagram(header ProtoVHeader, payload []byte) []byte {
	datagram := new(bytes.Buffer)
	binary.Write(datagram, binary.BigEndian, header)
	datagram.Write(payload)
	return datagram.Bytes()
}

func unpackProtoVDatagram(data []byte, addr *net.UDPAddr) (*RawMessage, error) {
	if len(data) < ProtoVHeaderSize {
		return nil, fmt.Errorf("Received datagram smaller than header size")
	}
	var header ProtoVHeader
	headerReader := bytes.NewReader(data[:ProtoVHeaderSize])
	if err := binary.Read(headerReader, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("Error parsing header: %v", err)
	}
	payload := data[ProtoVHeaderSize:]
	return &RawMessage{
		Header:    &header,
		Payload:   payload,
		FromAddr:  addr,
		rawPacket: data,
	}, nil
}

func (r *RawMessage) RawPacket() []byte {
	return r.rawPacket
}

func (r *RawMessage) EdgeMACAddr() string {
	return r.Header.GetSrcMACAddr().String()
}
func (r *RawMessage) CommunityHash() uint32 {
	return r.Header.CommunityID
}

func NewRawMessage(packet []byte, addr *net.UDPAddr) (*RawMessage, error) {
	if len(packet) < ProtoVHeaderSize {
		return nil, fmt.Errorf("packet too short from %v", addr)
	}

	version := packet[0]
	if version != VersionV {
		return nil, fmt.Errorf("not a VersionV packet from %v", addr)
	}

	return unpackProtoVDatagram(packet, addr)
}

type PingMessage struct {
	RawMsg        *RawMessage
	EdgeMACAddr   string
	CommunityHash uint32
	CheckID       string
	IsPong        bool
	DestMACAddr   string
}

// Payload Format request: PING sharedid
//
//	response: PONG sharedid
func (r *RawMessage) ToPingMessage() (*PingMessage, error) {
	if r.Header.PacketType != spec.TypePing {
		return nil, fmt.Errorf("not a TypePing packet")
	}
	parts := strings.Fields(string(r.Payload))
	if len(parts) < 2 || (parts[0] != "PING" && parts[0] != "PONG") {
		return nil, fmt.Errorf("invalid payload format despite TypePing")
	}
	isPong := (parts[0] == "PONG")
	return &PingMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		DestMACAddr:   r.Header.GetDstMACAddr().String(),
		CheckID:       parts[1],
		IsPong:        isPong,
	}, nil
}

func (pmsg *PingMessage) ToPacket() []byte {
	return pmsg.RawMsg.RawPacket()
}

type DataMessage struct {
	RawMsg        *RawMessage
	EdgeMACAddr   string
	CommunityHash uint32
	DestMACAddr   string
}

func (d *DataMessage) ToPacket() []byte {
	return d.RawMsg.rawPacket
}

func (r *RawMessage) ToDataMessage() (*DataMessage, error) {
	if r.Header.PacketType != spec.TypeData {
		return nil, fmt.Errorf("not a TypeData packet")
	}

	var destaddr string
	if destmac := r.Header.GetDstMACAddr(); destmac != nil {
		destaddr = destmac.String()
	}

	return &DataMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		DestMACAddr:   destaddr,
	}, nil

}

type Message[T netstruct.PacketTyped] struct {
	*RawMessage
	Msg T
}

type GenericStruct[T netstruct.PacketTyped] struct {
	V T
}

func (g *GenericStruct[T]) Encode() ([]byte, error) {
	return codec.NewCodec[T]().Encode(g.V)
}

func (g *GenericStruct[T]) Parse(data []byte) (*T, error) {
	return codec.NewCodec[T]().Decode(data)
}

func (g *GenericStruct[T]) ToMessage(r *RawMessage) (*Message[T], error) {
	var x T
	if spec.PacketType(r.Header.PacketType) != x.PacketType() {
		return nil, fmt.Errorf("not a %s packet", x.PacketType().String())
	}
	v, err := g.Parse(r.Payload)
	if err != nil {
		return nil, err
	}
	return &Message[T]{
		RawMessage: r,
		Msg:        *v,
	}, nil
}

func ToMessage[T netstruct.PacketTyped](r *RawMessage) (*Message[T], error) {
	s := GenericStruct[T]{}
	return s.ToMessage(r)
}

func Encode[T any](s T) ([]byte, error) {
	return codec.NewCodec[T]().Encode(s)
}

func MessageFromPacket[T netstruct.PacketTyped](packet []byte, addr *net.UDPAddr) (*Message[T], error) {
	rawMsg, err := NewRawMessage(packet, addr)
	if err != nil {
		return nil, fmt.Errorf("Edge: error while parsing MessageFromPacket: %w", err)
	}
	return ToMessage[T](rawMsg)
}

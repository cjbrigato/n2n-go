package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"n2n-go/pkg/protocol/codec"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
)

type RawMessage struct {
	Header    *ProtoVHeader
	Payload   []byte
	FromAddr  *net.UDPAddr
	rawPacket []byte
}

func PackProtoVDatagram(header *ProtoVHeader, payload []byte) []byte {
	datagram := new(bytes.Buffer)
	binary.Write(datagram, binary.BigEndian, header)
	datagram.Write(payload)
	return datagram.Bytes()
}

func UnpackProtoVDatagram(packet []byte) (*ProtoVHeader, []byte, error) {
	if len(packet) < ProtoVHeaderSize {
		return nil, nil, fmt.Errorf("packet too short")
	}

	version := packet[0]
	if version != VersionV {
		return nil, nil, fmt.Errorf("not a VersionV packet")
	}

	var header ProtoVHeader
	headerReader := bytes.NewReader(packet[:ProtoVHeaderSize])
	if err := binary.Read(headerReader, binary.BigEndian, &header); err != nil {
		return nil, nil, fmt.Errorf("Error parsing header: %v", err)
	}
	return &header, packet[ProtoVHeaderSize:], nil
}

func (r *RawMessage) RawPacket() []byte {
	return r.rawPacket
}

func (r *RawMessage) HasSrcMACAddr() bool {
	return r.Header.HasSrcMACAddr()
}

func (r *RawMessage) HasDstMACAddr() bool {
	return r.Header.HasDstMACAddr()
}

func (r *RawMessage) DestMACAddr() string {
	if r.HasDstMACAddr() {
		return r.Header.GetDstMACAddr().String()
	}
	return ""
}

func (r *RawMessage) EdgeMACAddr() string {
	if r.HasSrcMACAddr() {
		return r.Header.GetSrcMACAddr().String()
	}
	return ""
}
func (r *RawMessage) CommunityHash() uint32 {
	return r.Header.CommunityID
}

func (r *RawMessage) IsFromSupernode() bool {
	return r.Header.IsFromSupernode()
}

func FlagPacketFromSupernode(packet []byte) ([]byte, error) {
	header, payload, err := UnpackProtoVDatagram(packet)
	if err != nil {
		return nil, err
	}
	header.SetFromSupernode(true)
	return PackProtoVDatagram(header, payload), nil
}

func NewRawMessage(packet []byte, addr *net.UDPAddr) (*RawMessage, error) {

	header, payload, err := UnpackProtoVDatagram(packet)
	if err != nil {
		return nil, fmt.Errorf("%w from %v", err, addr)
	}
	return &RawMessage{
		Header:    header,
		Payload:   payload,
		FromAddr:  addr,
		rawPacket: packet,
	}, nil
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
		return nil, fmt.Errorf("edge: error while parsing MessageFromPacket: %w", err)
	}
	return ToMessage[T](rawMsg)
}

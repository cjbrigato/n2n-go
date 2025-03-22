package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol/codec"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/protocol/spec"
	"net"
	"strings"
)

type RawMessage struct {
	Header    *ProtoVHeader
	Payload   []byte
	Addr      *net.UDPAddr
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
		Addr:      addr,
		rawPacket: data,
	}, nil
}

func (r *RawMessage) RawPacket() []byte {
	return r.rawPacket
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

type LeasesInfosMessage struct {
	RawMsg        *RawMessage
	CommunityName string
	CommunityHash uint32
	EdgeMACAddr   string
	IsRequest     bool
	LeasesInfos   netstruct.LeasesInfos
}

func (r *RawMessage) ToLeasesInfosMessage() (*LeasesInfosMessage, error) {
	if r.Header.PacketType != TypeLeasesInfos {
		return nil, fmt.Errorf("not a TypeLeasesInfos packet")
	}
	pil, err := netstruct.ParseLeasesInfos(r.Payload)
	if err != nil {
		return nil, err
	}
	return &LeasesInfosMessage{
		RawMsg:        r,
		CommunityName: pil.CommunityName,
		CommunityHash: r.Header.CommunityID,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		IsRequest:     pil.IsRequest,
		LeasesInfos:   *pil,
	}, nil
}

type P2PFullStateMessage struct {
	RawMsg        *RawMessage
	CommunityHash uint32
	EdgeMACAddr   string
	IsRequest     bool
	P2PFullState  p2p.P2PFullState
}

func (r *RawMessage) ToP2PFullStateMessage() (*P2PFullStateMessage, error) {
	if r.Header.PacketType != TypeP2PFullState {
		return nil, fmt.Errorf("not a TypeP2PFullState packet")
	}
	pil, err := p2p.ParseP2PFullState(r.Payload)
	if err != nil {
		return nil, err
	}
	return &P2PFullStateMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		IsRequest:     pil.IsRequest,
		P2PFullState:  *pil,
	}, nil
}

type P2PStateInfoMessage struct {
	RawMsg        *RawMessage
	CommunityHash uint32
	EdgeMACAddr   string
	PeerP2PInfos  p2p.PeerP2PInfos
}

func (r *RawMessage) ToP2PStateInfoMessage() (*P2PStateInfoMessage, error) {
	if r.Header.PacketType != TypeP2PStateInfo {
		return nil, fmt.Errorf("not a TypeP2PStateInfo packet")
	}
	pil, err := p2p.ParsePeerP2PInfos(r.Payload)
	if err != nil {
		return nil, err
	}
	return &P2PStateInfoMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		PeerP2PInfos:  *pil,
	}, nil
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
	if r.Header.PacketType != TypePing {
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

type PeerInfoMessage struct {
	RawMsg        *RawMessage
	CommunityHash uint32
	PeerInfoList  *p2p.PeerInfoList
}

func (r *RawMessage) ToPeerInfoMessage() (*PeerInfoMessage, error) {
	if r.Header.PacketType != TypePeerInfo {
		return nil, fmt.Errorf("not a TypePeerInfo packet")
	}
	pil, err := p2p.ParsePeerInfoList(r.Payload)
	if err != nil {
		return nil, err
	}
	return &PeerInfoMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		PeerInfoList:  pil,
	}, nil
}

type PeerRequestMessage struct {
	RawMsg        *RawMessage
	EdgeMACAddr   string
	CommunityHash uint32
	CommunityName string
}

// Payload Format: PEERREQUEST <CommunityName>
func (r *RawMessage) ToPeerRequestMessage() (*PeerRequestMessage, error) {
	if r.Header.PacketType != TypePeerRequest {
		return nil, fmt.Errorf("not a TypePeerRequest packet")
	}
	parts := strings.Fields(string(r.Payload))
	if len(parts) < 2 || parts[0] != "PEERREQUEST" {
		return nil, fmt.Errorf("invalid payload format despite TypePeerRequest")
	}

	return &PeerRequestMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		CommunityName: parts[1],
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
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
	if r.Header.PacketType != TypeData {
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

type HeartbeatMessage struct {
	RawMsg        *RawMessage
	EdgeMACAddr   string
	CommunityHash uint32
	CommunityName string
}

// Payload Format: HEARTBEAT <CommunityName>
func (r *RawMessage) ToHeartbeatMessage() (*HeartbeatMessage, error) {
	if r.Header.PacketType != TypeHeartbeat {
		return nil, fmt.Errorf("not a TypeHeartbeat packet")
	}
	parts := strings.Fields(string(r.Payload))
	if len(parts) < 2 || parts[0] != "HEARTBEAT" {
		return nil, fmt.Errorf("invalid payload format despite TypeHeartbeat")
	}

	return &HeartbeatMessage{
		RawMsg:        r,
		CommunityHash: r.Header.CommunityID,
		CommunityName: parts[1],
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
	}, nil

}

type AckMessage struct {
	RawMsg      *RawMessage
	EdgeMACAddr string
}

func (r *RawMessage) ToAckMessage() (*AckMessage, error) {
	if r.Header.PacketType != TypeAck {
		return nil, fmt.Errorf("not a TypeAck packet")
	}
	return &AckMessage{
		RawMsg:      r,
		EdgeMACAddr: r.Header.GetSrcMACAddr().String(),
	}, nil
}

type UnregisterMessage struct {
	RawMsg        *RawMessage
	EdgeMACAddr   string
	CommunityHash uint32
}

func (r *RawMessage) ToUnregisterMessage() (*UnregisterMessage, error) {
	if r.Header.PacketType != TypeUnregisterRequest {
		return nil, fmt.Errorf("not a TypeUnregister packet")
	}
	return &UnregisterMessage{
		RawMsg:        r,
		EdgeMACAddr:   r.Header.GetSrcMACAddr().String(),
		CommunityHash: r.Header.CommunityID,
	}, nil
}

type RegisterRequestMessage struct {
	RawMsg          *RawMessage
	CommunityHash   uint32
	CommunityName   string
	EdgeDesc        string
	EdgeMACAddr     string
	RegisterRequest *netstruct.RegisterRequest
}

func (r *RawMessage) ToRegisterRequestMessage() (*RegisterRequestMessage, error) {
	if r.Header.PacketType != TypeRegisterRequest {
		return nil, fmt.Errorf("not a TypeRegister packet")
	}
	rreq, err := netstruct.ParseRegisterRequest(r.Payload)
	if err != nil {
		return nil, err
	}
	return &RegisterRequestMessage{
		RawMsg:          r,
		CommunityHash:   r.Header.CommunityID,
		CommunityName:   rreq.CommunityName,
		EdgeDesc:        rreq.EdgeDesc,
		EdgeMACAddr:     r.Header.GetSrcMACAddr().String(),
		RegisterRequest: rreq,
	}, nil

}

type PMessage[T netstruct.Messageable[T]] struct {
	RawMsg *RawMessage
	Msg    T
}

func ToPMessage[T netstruct.Messageable[T]](r *RawMessage) (*PMessage[T], error) {
	var x T
	if spec.PacketType(r.Header.PacketType) != x.PacketType() {
		return nil, fmt.Errorf("not a %s packet", x.PacketType().String())
	}
	v, err := x.Parse(r.Payload)
	if err != nil {
		return nil, err
	}
	return &PMessage[T]{
		RawMsg: r,
		Msg:    v,
	}, nil
}

type Message[T netstruct.PacketTyped] struct {
	RawMsg *RawMessage
	Msg    T
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
		RawMsg: r,
		Msg:    *v,
	}, nil
}

func ToMessage[T netstruct.PacketTyped](r *RawMessage) (*Message[T], error) {
	s := GenericStruct[T]{}
	return s.ToMessage(r)
}

func Encode[T any](s T) ([]byte, error) {
	return codec.NewCodec[T]().Encode(s)
}

/*
type GMessage[P netstruct.PacketTyped] struct {
	RawMsg *RawMessage
	Msg P
}

func ToGMessage[P netstruct.PacketTyped](r *RawMessage)(*GMessage[P],error){
	var x netstruct.GenericStruct[P]
	if spec.PacketType(r.Header.PacketType) != x.PacketType() {
		return nil, fmt.Errorf("not a %s packet", x.PacketType().String())
	}
	v, err := x.Parse(r.Payload)
	if err != nil {
		return nil, err
	}
	return &GMessage[P]{
		RawMsg: r,
		Msg: v,
	}, nil
}*/

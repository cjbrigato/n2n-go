package p2p

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net"
	"net/netip"
)


type PeerInfoEventType uint8

const (
	TypeList       PeerInfoEventType = 1
	TypeRegister   PeerInfoEventType = 2
	TypeUnregister PeerInfoEventType = 3
)

type ReachablesP4P struct {
	Reachables []PeerInfo
	For        PeerInfo
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	//PrivSocket *net.UDPAddr     `json:"privSocket"`
	Community string `json:"community"`
	Desc      string `json:"desc"`
}

type PeerInfoList struct {
	PeerInfos []PeerInfo
	EventType PeerInfoEventType
}

func (pil *PeerInfoList) Encode() ([]byte, error) {
	return encodePeerInfos(*pil)
}

func ParsePeerInfoList(data []byte) (*PeerInfoList, error) {
	pil, err := decodePeerInfos(data)
	if err != nil {
		return nil, err
	}
	return pil, nil
}

func encodePeerInfos(pil PeerInfoList) ([]byte, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer) // Will write to network.
	err := enc.Encode(pil)
	if err != nil {
		return nil, fmt.Errorf("error while encore PeerInfos: %w", err)
	}
	return buffer.Bytes(), nil
}

func decodePeerInfos(data []byte) (*PeerInfoList, error) {
	r := bytes.NewReader(data)
	enc := gob.NewDecoder(r) // Will write to network.
	pil := &PeerInfoList{}
	err := enc.Decode(pil)
	if err != nil {
		return nil, fmt.Errorf("error while decoding PeerInfos: %w", err)
	}
	return pil, nil
}

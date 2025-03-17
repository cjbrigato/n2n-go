package peer

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const PeerInfoPacketMinSize = 4 + 6 + 4 + 2 + 4 + 1

type PeerInfoEventType uint8

const (
	TypeList       PeerInfoEventType = 1
	TypeRegister   PeerInfoEventType = 2
	TypeUnregister PeerInfoEventType = 3
)

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	Community string           `json:"community"`
	Desc      string           `json:"desc"`
}

type PeerInfosPacket struct {
	VirtualIP     [4]byte
	MACAddr       [6]byte
	PublicIP      [4]byte
	Port          uint16
	CommunityHash uint32
	DescLen       uint8
	DescStr       []byte
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

func (pi *PeerInfosPacket) UnmarshalBinary(data []byte) error {
	if len(data) < PeerInfoPacketMinSize {
		return errors.New("insufficient data for PeerInfosPacket")
	}
	copy(pi.VirtualIP[:], data[0:4])
	copy(pi.MACAddr[:], data[4:10])
	copy(pi.PublicIP[:], data[10:14])
	pi.Port = binary.BigEndian.Uint16(data[14:16])
	pi.CommunityHash = binary.BigEndian.Uint32(data[16:20])
	pi.DescLen = data[20]
	if pi.DescLen > 0 {
		if len(data) >= PeerInfoPacketMinSize+int(pi.DescLen) {
			pi.DescStr = data[21:]
		} else {
			return fmt.Errorf("Wrong Desclen agaist packet size")
		}
	}
	return nil
}

// Edge represents a registered edge.
type Edge struct {
	Desc          string     // from header.SourceID
	PublicIP      net.IP     // Public IP address
	Port          int        // UDP port
	Community     string     // Community name
	VirtualIP     netip.Addr // Virtual IP assigned within the community
	VNetMaskLen   int        // CIDR mask length for the virtual network
	LastHeartbeat time.Time  // Time of last heartbeat
	LastSequence  uint16     // Last sequence number received
	MACAddr       string     // MAC address provided during registration
}

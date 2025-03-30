package protocol

import (
	"fmt"
	"net"
)

const (
	ProtoVFuzeSize = 7
	VersionVFuze   = 0x51
)

// ProtoVHeader is an optimized network protocol header with reduced overhead
type ProtoVFuzeHeader struct {
	// Basic packet information (4 bytes)
	Version uint8   // Protocol version
	DestID  [6]byte // Destination identifier (Dest TAP MACAddr)
}

func VFuzeHeaderBytes(dst net.HardwareAddr) []byte {
	buf := make([]byte, ProtoVFuzeSize)
	buf[0] = VersionVFuze
	copy(buf[1:7], dst[:6])
	return buf
}

func VFuzePacketDestMACAddr(packet []byte) (net.HardwareAddr,error){
	if len(packet) < ProtoVFuzeSize {
		return nil, fmt.Errorf("short packet not vFuze")
	}
	if packet[0] != VersionVFuze {
		return nil, fmt.Errorf("packet is not VersionVFuze")
	}
	return net.HardwareAddr(packet[1:7]),nil
}

package protocol

import "net"

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

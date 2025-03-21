package protocol

import (
	"bytes"
	"encoding/binary"
	"math"
	"net"
	"sync"
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

const (
	ProtoVFragSize      = 12
	VersionVFrag        = 0x61
	VFragPayloadMaxSize = 1024
)

type ProtoVFragHeader struct {
	Version       uint8 //0x61
	PacketType    PacketType
	SrcID         [6]byte
	FragmentID    uint16 // Unique ID for each fragmented message
	FragmentIndex uint8  // Index of this fragment (0-based)
	FragmentTotal uint8  // Total number of fragments
}

type ProtoVFragPacket struct {
	h ProtoVFragHeader
	p []byte
}

var FragIDMu sync.RWMutex
var FragIDsTracker = make(map[string]uint16)

func MakeVFragPackets(t PacketType, src net.HardwareAddr, payload []byte) [][]byte {
	var frags [][]byte
	frsrcid := src.String()
	FragIDMu.Lock()
	frid, ok := FragIDsTracker[frsrcid]
	if !ok {
		FragIDsTracker[frsrcid] = 0
		frid = 0
	} else {
		frid = frid + 1
		FragIDsTracker[frsrcid] = frid
	}
	FragIDMu.Unlock()
	header := ProtoVFragHeader{
		Version:       VersionVFrag,
		PacketType:    t,
		SrcID:         [6]byte{},
		FragmentID:    frid,
		FragmentTotal: 1,
	}
	copy(header.SrcID[0:6], src[:6])

	if len(payload) <= VFragPayloadMaxSize {
		datagram := packDatagram(header, payload)
		frags = append(frags, datagram)
		return frags
	}
	totalFragments := uint8(math.Ceil(float64(len(payload)) / float64(VFragPayloadMaxSize)))
	for i := uint8(0); i < totalFragments; i++ {
		// Calculate fragment start and end
		start := int(i) * VFragPayloadMaxSize
		end := int(start) + VFragPayloadMaxSize
		if end > len(payload) {
			end = len(payload)
		}

		// Prepare fragment header
		fragmentHeader := header
		fragmentHeader.FragmentID = frid
		fragmentHeader.FragmentIndex = i
		fragmentHeader.FragmentTotal = totalFragments

		// Pack header and fragment payload
		fragmentPayload := payload[start:end]
		datagram := packDatagram(fragmentHeader, fragmentPayload)
		frags = append(frags, datagram)
	}
	return frags
}

func packDatagram(header ProtoVFragHeader, payload []byte) []byte {
	// Create a buffer for the complete datagram
	datagram := new(bytes.Buffer)

	// Write header to the buffer
	binary.Write(datagram, binary.BigEndian, header)

	// Write payload to the buffer
	datagram.Write(payload)

	return datagram.Bytes()
}

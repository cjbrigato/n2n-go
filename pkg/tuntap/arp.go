package tuntap

import (
	"encoding/binary"
	"fmt"
	"net"
)

// SendGratuitousARP sends a gratuitous ARP reply for the given IP address
// using the MAC address of this interface.
func (i *Interface) SendGratuitousARP(vip net.IP) error {
	if vip.To4() == nil {
		return fmt.Errorf("SendGratuitousARP currently only supports IPv4")
	}

	// Get the source MAC address from the interface itself
	srcMAC := i.HardwareAddr() // Use the existing method
	if srcMAC == nil {
		// Handle cases where MAC address might not be available yet
		// Maybe the interface isn't fully up/configured?
		return fmt.Errorf("cannot send gratuitous ARP: hardware address not available for interface %s", i.Name())
	}
	if len(srcMAC) < 6 {
		// It's possible HardwareAddr() returns a short MAC sometimes before full config
		return fmt.Errorf("cannot send gratuitous ARP: invalid hardware address length (%d) for interface %s", len(srcMAC), i.Name())
	}
	// Ensure we use only 6 bytes if HardwareAddr() somehow returns more
	srcMAC = srcMAC[:6]

	// --- Build the Ethernet frame (Common logic) ---
	broadcastMAC := net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	ethTypeARP := []byte{0x08, 0x06} // ARP EtherType

	arpPacket := make([]byte, 28)
	binary.BigEndian.PutUint16(arpPacket[0:2], 1)      // Hardware type: Ethernet
	binary.BigEndian.PutUint16(arpPacket[2:4], 0x0800) // Protocol type: IPv4
	arpPacket[4] = 6                                   // Hardware size
	arpPacket[5] = 4                                   // Protocol size
	binary.BigEndian.PutUint16(arpPacket[6:8], 2)      // Opcode: Reply
	copy(arpPacket[8:14], srcMAC)                      // Sender MAC
	copy(arpPacket[14:18], vip.To4())                  // Sender IP
	copy(arpPacket[18:24], srcMAC)                     // Target MAC (same as sender)
	copy(arpPacket[24:28], vip.To4())                  // Target IP (same as sender)

	ethFrame := make([]byte, 14+28)
	copy(ethFrame[0:6], broadcastMAC) // Dest MAC
	copy(ethFrame[6:12], srcMAC)      // Src MAC
	copy(ethFrame[12:14], ethTypeARP) // EtherType
	copy(ethFrame[14:], arpPacket)    // Payload
	// --- End frame building ---

	// Call the platform-specific sending function
	return i.sendRawFrame(ethFrame)
}

// sendRawFrame is implemented in platform-specific files (arp_linux.go, arp_windows.go)
// This internal method handles the actual OS-level sending.
// We keep it separate to isolate the build constraints cleanly.
// func (i *Interface) sendRawFrame(frame []byte) error

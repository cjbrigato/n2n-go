package util

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
)

// htons converts a 16-bit integer from host to network byte order.
func htons(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}

// SendGratuitousARP sends a gratuitous ARP on the specified interface.
// It constructs an ARP reply where both sender and target IP/MAC are set to the provided values.
func SendGratuitousARP(ifaceName string, srcMAC net.HardwareAddr, vip net.IP) error {
	// Build the Ethernet header.
	broadcastMAC := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	ethTypeARP := []byte{0x08, 0x06} // ARP EtherType

	// Build the ARP packet (28 bytes).
	arpPacket := make([]byte, 28)
	// Hardware type: Ethernet (1)
	binary.BigEndian.PutUint16(arpPacket[0:2], 1)
	// Protocol type: IPv4 (0x0800)
	binary.BigEndian.PutUint16(arpPacket[2:4], 0x0800)
	// Hardware size: 6, Protocol size: 4
	arpPacket[4] = 6
	arpPacket[5] = 4
	// Opcode: ARP Reply (2)
	binary.BigEndian.PutUint16(arpPacket[6:8], 2)
	// Sender MAC: srcMAC (6 bytes)
	copy(arpPacket[8:14], srcMAC)
	// Sender IP: vip (4 bytes)
	copy(arpPacket[14:18], vip.To4())
	// Target MAC: srcMAC (for gratuitous ARP, same as sender)
	copy(arpPacket[18:24], srcMAC)
	// Target IP: vip (4 bytes)
	copy(arpPacket[24:28], vip.To4())

	// Construct the full Ethernet frame (14 bytes header + 28 bytes ARP payload).
	ethFrame := make([]byte, 14+28)
	// Destination MAC: broadcast
	copy(ethFrame[0:6], broadcastMAC)
	// Source MAC: srcMAC
	copy(ethFrame[6:12], srcMAC)
	// EtherType: ARP
	copy(ethFrame[12:14], ethTypeARP)
	// ARP packet payload.
	copy(ethFrame[14:], arpPacket)

	// Open a raw socket.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(0x0003))) // ETH_P_ALL = 0x0003
	if err != nil {
		return fmt.Errorf("failed to open raw socket: %v", err)
	}
	defer syscall.Close(fd)

	// Get interface by name.
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %v", ifaceName, err)
	}

	// Construct the sockaddr_ll.
	sll := &syscall.SockaddrLinklayer{
		Ifindex: iface.Index,
		Halen:   6,
		Addr:    [8]byte{broadcastMAC[0], broadcastMAC[1], broadcastMAC[2], broadcastMAC[3], broadcastMAC[4], broadcastMAC[5]},
	}

	// Send the Ethernet frame.
	if err := syscall.Sendto(fd, ethFrame, 0, sll); err != nil {
		return fmt.Errorf("failed to send gratuitous ARP: %v", err)
	}
	return nil
}

//go:build linux

package tuntap

import (
	"fmt"
	"net"
	"syscall"
)

// htons converts a 16-bit integer from host to network byte order.
func htons(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}

// sendRawFrame sends an Ethernet frame using AF_PACKET on Linux.
func (i *Interface) sendRawFrame(frame []byte) error {
	ifaceName := i.Name() // Get name from the interface

	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(htons(syscall.ETH_P_ARP)))
	if err != nil {
		return fmt.Errorf("linux: failed to open raw socket (AF_PACKET): %w", err)
	}
	defer syscall.Close(fd)

	netIface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("linux: failed to get interface %s details: %w", ifaceName, err)
	}

	// Destination MAC is needed for SockaddrLinklayer address field
	var destMAC net.HardwareAddr
	if len(frame) >= 6 {
		destMAC = frame[0:6]
	} else {
		return fmt.Errorf("linux: frame too short to extract destination MAC")
	}

	sll := &syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ARP),
		Ifindex:  netIface.Index,
		Halen:    uint8(len(destMAC)),
	}
	copy(sll.Addr[:], destMAC) // Copy destination MAC into Addr

	if err := syscall.Sendto(fd, frame, 0, sll); err != nil {
		return fmt.Errorf("linux: failed to send frame via AF_PACKET on %s: %w", ifaceName, err)
	}
	return nil
}

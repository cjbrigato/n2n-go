//go:build linux

package tuntap

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"n2n-go/pkg/log" // Assuming this logging package exists
	"github.com/vishvananda/netlink" // Import the netlink library
)

const DefaultMTU = 1420 // Or your desired default MTU

// ConfigureInterface uses netlink to configure the interface on Linux
// This is the original implementation from link.go
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error {
	ifName := i.Name() // Assumes Interface has Name() method
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface %q: %w", ifName, err)
	}

	// Set MAC address if provided
	if macAddr != "" {
		hwAddr, err := net.ParseMAC(macAddr)
		if err != nil {
			return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
		}
		if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
			// EOPNOTSUPP might happen on some TUN devices, maybe don't error?
			if !errors.Is(err, syscall.EOPNOTSUPP) {
				return fmt.Errorf("failed to set MAC address %q on interface %q: %w", macAddr, ifName, err)
			}
			log.Printf("Warning: cannot set MAC Address %s on interface %s: %v", macAddr, ifName, err)
		} else {
			log.Printf("Set MAC address %s on interface %s", macAddr, ifName)
		}
	}

	// Set IP address
	addr, err := netlink.ParseAddr(ipCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse IP address %q: %w", ipCIDR, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		// Ignore EEXIST error if the address is already present
		if !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("failed to add IP address %q to interface %q: %w", ipCIDR, ifName, err)
		}
		log.Printf("IP address %q already exists on interface %q", ipCIDR, ifName)
	} else {
		log.Printf("Added IP address %s to interface %s", ipCIDR, ifName)
	}

	// Set MTU
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fmt.Errorf("failed to set MTU %d on interface %q: %w", mtu, ifName, err)
	}
	log.Printf("Set MTU %d on interface %s", mtu, ifName)

	// Bring the interface up
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up interface %q: %w", ifName, err)
	}
	log.Printf("Brought up interface %s", ifName)

	return nil
}

// IfUp provides a simpler way to bring the interface up with IP and default MTU
func (i *Interface) IfUp(ipCIDR string) error {
	return i.ConfigureInterface("", ipCIDR, DefaultMTU)
}

// IfMac provides a way to set only the MAC address (if possible)
func (i *Interface) IfMac(macAddr string) error {
	ifName := i.Name()
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface %q: %w", ifName, err)
	}

	hwAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
	}

	if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
		if !errors.Is(err, syscall.EOPNOTSUPP) {
			return fmt.Errorf("failed to set MAC address %q on interface %q: %w", macAddr, ifName, err)
		}
		log.Printf("Warning: cannot set MAC Address %s on interface %s: %v", macAddr, ifName, err)
	} else {
		log.Printf("Set MAC address %s on interface %s", macAddr, ifName)
	}

	return nil
}
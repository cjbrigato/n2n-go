package tuntap

import (
	"errors"
	"fmt"
	"n2n-go/pkg/log"
	"net"
	"syscall"

	"github.com/vishvananda/netlink" // Import the netlink library
)

// DefaultMTU is a common MTU value, especially for tunnels.
const DefaultMTU = 1420

// ConfigureInterface sets the MAC address, IP address, MTU, and brings up the network interface.
// ipCIDR must be in CIDR notation, e.g., "192.168.1.5/24".
// macAddr can be an empty string if setting the MAC is not desired.
// mtu value will be used to set the MTU; if <= 0, DefaultMTU (1420) is used.
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error {
	ifName := i.Name()
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface %q: %w", ifName, err)
	}

	// Set MAC Address (Hardware Address) if provided.
	if macAddr != "" {
		hwAddr, err := net.ParseMAC(macAddr)
		if err != nil {
			return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
		}
		if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
			// Don't fail hard on EOPNOTSUPP, some virtual devices (like wireguard)
			// may not support setting MAC address.
			if !errors.Is(err, syscall.EOPNOTSUPP) {
				return fmt.Errorf("failed to set MAC address %q on interface %q: %w", macAddr, ifName, err)
			}
			log.Printf("cannot set MAC Address %s on interface %s", macAddr, ifName)
		} else {
			log.Printf("Set MAC address %s on interface %s", macAddr, ifName)
		}
	}

	addr, err := netlink.ParseAddr(ipCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse IP address %q: %w", ipCIDR, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("failed to add IP address %q to interface %q: %w", ipCIDR, ifName, err)
		}
		log.Printf("Cannot set ip address %q to interface %q: %v", ipCIDR, ifName, err)
	} else {
		log.Printf("Added IP address %s to interface %s", ipCIDR, ifName)
	}
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fmt.Errorf("failed to set MTU %d on interface %q: %w", mtu, ifName, err)
	}
	log.Printf("Set MTU %d on interface %s", mtu, ifName)

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up interface %q: %w", ifName, err)
	}
	log.Printf("Brought up interface %s", ifName)

	return nil
}

// IfUp brings up the interface, assigns IP, and sets default MTU.
// ipCIDR must be in CIDR notation (e.g., "192.168.1.5/24").
func (i *Interface) IfUp(ipCIDR string) error {
	// Use the combined function, setting MAC to empty and MTU to default.
	return i.ConfigureInterface("", ipCIDR, DefaultMTU)
}

// IfMac sets the MAC address for the interface.
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
		return fmt.Errorf("failed to set MAC address %q on interface %q: %w", macAddr, ifName, err)
	}
	log.Printf("Set MAC address %s on interface %s", macAddr, ifName)
	return nil
}

//go:build netbsd

package tuntap

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"n2n-go/pkg/log" // Assuming logging package
)

const DefaultMTU = 1420 // Default MTU for NetBSD TAP (adjust if needed)

// runCommand executes a command and logs output/errors.
func runCommand(cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)
	log.Printf("Executing: %s %s", cmdName, strings.Join(args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error executing command: %v\nOutput:\n%s", err, string(output))
		return fmt.Errorf("command '%s %s' failed: %w\nOutput: %s", cmdName, strings.Join(args, " "), err, string(output))
	}
	log.Printf("Command output:\n%s", string(output))
	return nil
}

// ConfigureInterface uses `ifconfig` to configure the interface on NetBSD.
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error {
	if i.Iface == nil {
		return fmt.Errorf("underlying device is nil")
	}
	ifName := i.Name() // Get the interface name (e.g., "tap0")

	// Set MAC address if provided
	// Note: NetBSD's ifconfig might use 'link' or 'lladdr' instead of 'ether'
	// Check 'man ifconfig' on NetBSD. Assuming 'link' for now.
	if macAddr != "" {
		hwAddr, err := net.ParseMAC(macAddr)
		if err != nil {
			return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
		}
		// Use 'link' keyword on NetBSD for setting MAC
		err = runCommand("ifconfig", ifName, "link", hwAddr.String())
		if err != nil {
			log.Printf("Warning: Failed to set MAC address %s on %s: %v. Check ifconfig syntax/permissions.", hwAddr.String(), ifName, err)
			// Decide whether to return error or just warn
			// return fmt.Errorf("failed to set MAC address: %w", err)
		} else {
			log.Printf("Attempted to set MAC address %s on interface %s", hwAddr.String(), ifName)
		}
	}

	// Set IP address and netmask
	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse IP CIDR %q: %w", ipCIDR, err)
	}
	if ip.To4() == nil {
		// TODO: Add IPv6 support for ifconfig if needed
		return fmt.Errorf("netbsd: ConfigureInterface currently only supports IPv4 addresses")
	}
	netmask := ipNet.Mask
	netmaskStr := fmt.Sprintf("%d.%d.%d.%d", netmask[0], netmask[1], netmask[2], netmask[3])

	// NetBSD ifconfig typically uses 'inet <ip> netmask <mask>'
	// Adding an IP might implicitly bring it up or require 'up' later.
	// Using 'alias' might be needed if multiple IPs are on the interface, but
	// for a typical TAP setup, directly setting the address is common.
	err = runCommand("ifconfig", ifName, "inet", ip.String(), "netmask", netmaskStr)
	if err != nil {
		// Check if the error indicates the address already exists? ifconfig might not have a specific error code.
		// If the command failed, return the error.
		return fmt.Errorf("failed to set IP address %s netmask %s: %w", ip.String(), netmaskStr, err)
	}
	log.Printf("Set IP address %s netmask %s on interface %s", ip.String(), netmaskStr, ifName)

	// Set MTU
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	err = runCommand("ifconfig", ifName, "mtu", fmt.Sprintf("%d", mtu))
	if err != nil {
		return fmt.Errorf("failed to set MTU %d: %w", mtu, err)
	}
	log.Printf("Set MTU %d on interface %s", mtu, ifName)

	// Bring the interface up (might be redundant if setting IP did it)
	err = runCommand("ifconfig", ifName, "up")
	if err != nil {
		return fmt.Errorf("failed to bring up interface %s: %w", ifName, err)
	}
	log.Printf("Brought up interface %s", ifName)

	return nil
}

// IfUp provides a simpler way to bring the interface up with IP and default MTU.
func (i *Interface) IfUp(ipCIDR string) error {
	return i.ConfigureInterface("", ipCIDR, DefaultMTU)
}

// IfMac provides a way to set only the MAC address (if possible).
func (i *Interface) IfMac(macAddr string) error {
	if i.Iface == nil {
		return fmt.Errorf("underlying device is nil")
	}
	ifName := i.Name()

	hwAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
	}

	// Use 'link' on NetBSD
	err = runCommand("ifconfig", ifName, "link", hwAddr.String())
	if err != nil {
		log.Printf("Warning: Failed to set MAC address %s on %s: %v.", hwAddr.String(), ifName, err)
		// return fmt.Errorf("failed to set MAC address: %w", err) // Optional: return error
	} else {
		log.Printf("Attempted to set MAC address %s on interface %s", hwAddr.String(), ifName)
	}
	return nil // Return nil even on warning, or return err if strictness is required
}

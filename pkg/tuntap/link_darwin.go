//go:build darwin

package tuntap

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"n2n-go/pkg/log" // Assuming logging package
)

const DefaultMTU = 1420 // Default MTU for Darwin TAP

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

// ConfigureInterface uses `ifconfig` to configure the interface on Darwin.
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error {
	if i.Iface == nil {
		return fmt.Errorf("underlying device is nil")
	}
	ifName := i.Name() // Get the interface name (e.g., "tap0")

	// Set MAC address if provided
	if macAddr != "" {
		// Note: Setting MAC on TAP might not always work or persist depending on driver/permissions
		hwAddr, err := net.ParseMAC(macAddr)
		if err != nil {
			return fmt.Errorf("failed to parse MAC address %q: %w", macAddr, err)
		}
		// Check for multicast/locally administered bits if needed (similar to Windows)
		// ... (validation logic can be added here) ...
		err = runCommand("ifconfig", ifName, "ether", hwAddr.String())
		if err != nil {
			log.Printf("Warning: Failed to set MAC address %s on %s: %v. This might be unsupported or require privileges.", hwAddr.String(), ifName, err)
			// Decide whether to return error or just warn
			// return fmt.Errorf("failed to set MAC address: %w", err)
		} else {
			log.Printf("Attempted to set MAC address %s on interface %s", hwAddr.String(), ifName)
			// Update internal state if necessary, though GetMACAddress reads dynamically
			// i.Iface.macAddr = hwAddr // Be careful if Device struct holds MAC state
		}
	}

	// Set IP address and netmask
	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse IP CIDR %q: %w", ipCIDR, err)
	}
	if ip.To4() == nil {
		// TODO: Add IPv6 support for ifconfig if needed
		return fmt.Errorf("darwin: ConfigureInterface currently only supports IPv4 addresses")
	}
	netmask := ipNet.Mask
	netmaskStr := fmt.Sprintf("%d.%d.%d.%d", netmask[0], netmask[1], netmask[2], netmask[3])

	// Use 'alias' to add the IP without removing existing ones (safer)
	err = runCommand("ifconfig", ifName, ip.String(), "netmask", netmaskStr, "alias")
	if err != nil {
		// Check if the error indicates the address already exists? ifconfig might not have a specific error code for this.
		// If the command failed, return the error.
		return fmt.Errorf("failed to add IP address %s netmask %s: %w", ip.String(), netmaskStr, err)
	}
	log.Printf("Added IP address %s netmask %s to interface %s", ip.String(), netmaskStr, ifName)

	// Set MTU
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	err = runCommand("ifconfig", ifName, "mtu", fmt.Sprintf("%d", mtu))
	if err != nil {
		return fmt.Errorf("failed to set MTU %d: %w", mtu, err)
	}
	log.Printf("Set MTU %d on interface %s", mtu, ifName)

	// Bring the interface up
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

	err = runCommand("ifconfig", ifName, "ether", hwAddr.String())
	if err != nil {
		log.Printf("Warning: Failed to set MAC address %s on %s: %v.", hwAddr.String(), ifName, err)
		// return fmt.Errorf("failed to set MAC address: %w", err) // Optional: return error
	} else {
		log.Printf("Attempted to set MAC address %s on interface %s", hwAddr.String(), ifName)
	}
	return nil // Return nil even on warning, or return err if strictness is required
}

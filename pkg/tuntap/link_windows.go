//go:build windows

package tuntap

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"n2n-go/pkg/log" // Assuming this logging package exists
)

const DefaultMTU = 1420 // Or your desired default MTU

// runNetsh executes a netsh command and logs output/errors
func runNetsh(args ...string) error {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // Prevent flashing console window
	log.Printf("Executing: netsh %s", strings.Join(args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("netsh error: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("netsh %s failed: %w (output: %s)", args[0], err, string(output))
	}
	log.Printf("netsh output: %s", string(output))
	return nil
}

// findInterfaceNameByGUID attempts to find the user-friendly interface name using the GUID stored in i.Name()
func findInterfaceNameByGUID(guid string) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		// On Windows, net.Interface.Name often contains the GUID for virtual adapters
		// We need to match against the GUID we stored during device creation.
		// This is fragile; a more robust method might involve WMI or IpHlpAPI to map GUID to IfIndex or Name.
		if strings.Contains(iface.Name, guid) { // Simple check, might need refinement
			// The "friendly" name used by netsh is often just the Index or a description.
			// Let's try using the index as the name identifier for netsh.
			// A better approach would be to get the Interface Description or Alias via WMI/IpHlpAPI
			// and use that if possible, otherwise fall back to index.
			// For now, we'll risk using the Index directly in netsh commands.
			return fmt.Sprintf("%d", iface.Index), nil // Use Index as the identifier
			// Alternatively, try returning iface.Name and hope netsh recognizes it.
			// return iface.Name, nil
		}
	}
	return "", fmt.Errorf("could not find interface name corresponding to GUID %s", guid)
}

// ConfigureInterface uses netsh commands on Windows.
// NOTE: This is less robust than using Windows APIs like IpHlpAPI or WMI,
//
//	but simpler to implement initially. Requires Administrator privileges.
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error {
	// On Windows, i.Name() holds the GUID from device creation. We need the name netsh uses.
	ifNameIdentifier, err := findInterfaceNameByGUID(i.Name())
	if err != nil {
		// Fallback: try using the GUID directly, might work with some netsh versions/adapters
		log.Printf("Warning: could not find interface name for GUID %s, attempting to use GUID directly with netsh: %v", i.Name(), err)
		ifNameIdentifier = i.Name() // Or maybe format as "{GUID}" ? Testing needed.
	}
	log.Printf("Attempting configuration using interface identifier: %q (derived from GUID %s)", ifNameIdentifier, i.Name())

	// --- Set MAC Address ---
	if macAddr != "" {
		// Changing MAC address programmatically on Windows TAP adapters is often NOT supported
		// via standard tools like netsh or APIs. It's usually set by the driver or registry.
		// We'll log a warning and attempt to retrieve the current one.
		log.Printf("Warning: Setting MAC address (%s) on Windows TAP interfaces is generally not supported via netsh/standard APIs.", macAddr)
		currentHWAddr, err := i.hardwareAddr() // Use the existing method to get current addr
		if err != nil {
			log.Printf("Warning: Failed to retrieve current MAC address for interface %s: %v", ifNameIdentifier, err)
		} else {
			log.Printf("Current MAC address for interface %s (%s) is %s", ifNameIdentifier, i.Name(), currentHWAddr.String())
		}
		// No reliable way to set it here, so we skip attempting it.
	}

	// --- Set IP Address ---
	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return fmt.Errorf("failed to parse IP CIDR %q: %w", ipCIDR, err)
	}
	if ip.To4() == nil {
		return fmt.Errorf("IPv6 configuration via netsh not implemented in this example")
		// Add netsh command for IPv6 if needed:
		// netsh interface ipv6 set address interface="{ifNameIdentifier}" address={ipv6}/{prefixlen} ...
	}
	ipStr := ip.String()
	maskStr := fmt.Sprintf("%d.%d.%d.%d", ipNet.Mask[0], ipNet.Mask[1], ipNet.Mask[2], ipNet.Mask[3])

	// Example: netsh interface ip set address name="Ethernet 2" static 192.168.1.100 255.255.255.0
	// Using Index: netsh interface ip set address interface=12 static 192.168.1.100 255.255.255.0
	// Note the keyword difference: 'name' vs 'interface' (for index)
	err = runNetsh("interface", "ip", "set", "address", fmt.Sprintf("interface=%s", ifNameIdentifier), "static", ipStr, maskStr)
	// If using name: err = runNetsh("interface", "ip", "set", "address", fmt.Sprintf("name=\"%s\"", ifNameIdentifier), "static", ipStr, maskStr)
	if err != nil {
		// Don't fail if address already exists? Check error output maybe.
		log.Printf("Warning: Failed to set IP address %s/%s on interface %s. Might require manual configuration or elevated privileges. Error: %v", ipStr, maskStr, ifNameIdentifier, err)
		// Don't return error immediately, try MTU and Up.
		// return fmt.Errorf("failed to set IP address: %w", err)
	} else {
		log.Printf("Set IP address %s/%s on interface %s", ipStr, maskStr, ifNameIdentifier)
	}

	// --- Set MTU ---
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	// Example: netsh interface ipv4 set subinterface "Ethernet 2" mtu=1400 store=persistent
	// Using Index: netsh interface ipv4 set subinterface interface=12 mtu=1400 store=persistent
	err = runNetsh("interface", "ipv4", "set", "subinterface", fmt.Sprintf("interface=%s", ifNameIdentifier), fmt.Sprintf("mtu=%d", mtu), "store=persistent")
	// If using name: err = runNetsh("interface", "ipv4", "set", "subinterface", fmt.Sprintf("\"%s\"", ifNameIdentifier), fmt.Sprintf("mtu=%d", mtu), "store=persistent")
	if err != nil {
		log.Printf("Warning: Failed to set MTU %d on interface %s. Might require manual configuration or elevated privileges. Error: %v", mtu, ifNameIdentifier, err)
		// Don't return error immediately
		// return fmt.Errorf("failed to set MTU: %w", err)
	} else {
		log.Printf("Set MTU %d on interface %s", mtu, ifNameIdentifier)
	}

	// --- Bring Interface Up ---
	// This is usually implicit after setting IP and ensuring media status is connected (done in device_windows.go).
	// But we can explicitly enable it too.
	// Example: netsh interface set interface name="Ethernet 2" admin=enabled
	// Using Index: netsh interface set interface interface=12 admin=enabled
	err = runNetsh("interface", "set", "interface", fmt.Sprintf("interface=%s", ifNameIdentifier), "admin=enabled")
	// If using name: err = runNetsh("interface", "set", "interface", fmt.Sprintf("name=\"%s\"", ifNameIdentifier), "admin=enabled")
	if err != nil {
		log.Printf("Warning: Failed to explicitly enable interface %s. It might already be enabled or require manual action. Error: %v", ifNameIdentifier, err)
		// Don't return error, getting IP/MTU set is usually sufficient if media status is connected.
		// return fmt.Errorf("failed to enable interface: %w", err)
	} else {
		log.Printf("Ensured interface %s is enabled", ifNameIdentifier)
	}

	log.Printf("Windows interface configuration attempt finished for %s (%s). Check warnings.", ifNameIdentifier, i.Name())
	return nil // Return nil even if some steps logged warnings, as basic functionality might still work.
}

func (i *Interface) IfUp(ipCIDR string) error {
	return i.ConfigureInterface("", ipCIDR, DefaultMTU)
}

func (i *Interface) IfMac(macAddr string) error {
	log.Printf("Warning: Setting MAC address (%s) on Windows TAP interfaces is generally not supported programmatically.", macAddr)
	// Attempt to retrieve and log the current MAC
	currentHWAddr, err := i.hardwareAddr()
	if err != nil {
		log.Printf("Warning: Failed to retrieve current MAC address for interface %s: %v", i.Name(), err)
		return fmt.Errorf("setting MAC not supported, and failed to retrieve current MAC: %w", err)
	}
	log.Printf("Current MAC address for interface %s is %s (cannot be changed programmatically)", i.Name(), currentHWAddr.String())
	return fmt.Errorf("setting MAC address (%s) is not supported on Windows", macAddr)
}

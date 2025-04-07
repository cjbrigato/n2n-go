package tuntap

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

// Interface struct remains the same
type Interface struct {
	Iface *Device // Holds platform-specific Device
}

// NewInterface creates a new TAP interface (TUN support limited/untested)
func NewInterface(name, mode string) (*Interface, error) {
	// Currently, only TAP mode is reliably supported across platforms
	// due to tap-windows6 limitations and simplicity.
	if mode != "tap" {
		return nil, fmt.Errorf("unsupported mode: %s (only 'tap' is reliably supported)", mode)
	}

	devType := TAP
	// If TUN support is added to device_windows.go later, adjust this logic.

	config := Config{
		Name:        name, // On Windows, this is preferred, actual name might be GUID
		DevType:     devType,
		Persist:     false, // Persist is likely Linux-only
		Owner:       -1,    // Owner/Group are Linux-only
		Group:       -1,
		Permissions: 0666, // Permissions are Linux-only
	}

	// Create calls the platform-specific implementation
	iface, err := Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s interface '%s': %w", mode, name, err)
	}

	// On Windows, the Iface.Name might be the GUID.
	// We might need a way to map this back to a user-friendly name if needed elsewhere.
	fmt.Printf("Successfully created interface. Platform: %s, Effective Name/ID: %s\n", runtime.GOOS, iface.Name)

	return &Interface{Iface: iface}, nil
}

// --- Methods mostly delegate to the underlying Device ---

func (i *Interface) Read(b []byte) (int, error) {
	return i.Iface.Read(b)
}

func (i *Interface) Write(b []byte) (int, error) {
	return i.Iface.Write(b)
}

func (i *Interface) Close() error {
	return i.Iface.Close()
}

// Name returns the effective name/identifier of the interface.
// On Linux, this is usually the assigned name (e.g., "tap0").
// On Windows, this is likely the adapter's GUID obtained during creation.
func (i *Interface) Name() string {
	return i.Iface.Name
}

// HardwareAddr attempts to get the MAC address using the standard net package.
// This should work on both platforms once the interface is configured and "up".
func (i *Interface) HardwareAddr() net.HardwareAddr {
	addr, err := i.hardwareAddr()
	if err != nil {
		// Log or handle error appropriately - might happen before config
		fmt.Fprintf(os.Stderr, "Warning: could not get hardware address for %s: %v\n", i.Name(), err)
		return nil
	}
	return addr
}

func (i *Interface) SetReadDeadline(t time.Time) error {
	return i.Iface.SetReadDeadline(t)
}

func (i *Interface) SetWriteDeadline(t time.Time) error {
	return i.Iface.SetWriteDeadline(t)
}

func (i *Interface) SetDeadline(t time.Time) error {
	return i.Iface.SetDeadline(t) // Delegate directly
}

// hardwareAddr is the internal implementation using net package
func (i *Interface) hardwareAddr() (net.HardwareAddr, error) {
	// Use the effective name/ID stored in i.Name()
	effectiveName := i.Name()

	// On Windows, net.InterfaceByName might fail with the GUID.
	// We need to iterate and find the interface whose name CONTAINS the GUID,
	// or better, match by index if we could reliably get it.
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		match := false
		if runtime.GOOS == "windows" {
			// Try matching if the interface name (often description or index on Windows)
			// or if the Go representation of the name (sometimes includes GUID) contains our ID.
			// This is heuristic and might need improvement.
			if strings.Contains(iface.Name, effectiveName) {
				match = true
			}
			// Alternative check: Does the OS-level name contain it? Requires different API.
			// Or match by index if link_windows could determine it.
		} else {
			// On Linux, names should match directly.
			if iface.Name == effectiveName {
				match = true
			}
		}

		if match {
			if iface.HardwareAddr == nil {
				// Interface found, but no MAC address (maybe not up yet?)
				// continue // Or return error?
				return nil, fmt.Errorf("interface %s (%s) found but has no hardware address", iface.Name, effectiveName)
			}
			// Return only the first 6 bytes if longer (common on Windows)
			if len(iface.HardwareAddr) >= 6 {
				return iface.HardwareAddr[:6], nil
			}
			return iface.HardwareAddr, nil // Return whatever length it is if < 6
		}
	}

	return nil, fmt.Errorf("failed to find interface with name/ID %s after checking %d interfaces", effectiveName, len(ifaces))
}

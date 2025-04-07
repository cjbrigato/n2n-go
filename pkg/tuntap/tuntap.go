package tuntap

import (
	"fmt"
	"net"
	"os" // For stderr logging example
	"runtime"
	"time"
)

// Interface struct remains the same
type Interface struct {
	Iface *Device // Holds platform-specific Device
}

// NewInterface creates a new TAP interface (TUN support limited/untested)
func NewInterface(name, mode string) (*Interface, error) {
	// Currently, only TAP mode is reliably supported across platforms
	if mode != "tap" {
		return nil, fmt.Errorf("unsupported mode: %s (only 'tap' is reliably supported)", mode)
	}

	devType := TAP

	// Config includes MACAddress field which will be used by device_windows.go
	config := Config{
		Name:        name, // Name might be used for matching if multiple TAP exist (not implemented)
		DevType:     devType,
		MACAddress:  "", // <<< --- CALLER NEEDS TO SET THIS IF DESIRED --- >>>
		Persist:     false,
		Owner:       -1,
		Group:       -1,
		Permissions: 0666,
	}

	// Create calls the platform-specific implementation
	// The Create function now handles registry setting and API discovery internally.
	iface, err := Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s interface '%s': %w", mode, name, err)
	}

	fmt.Printf("Successfully created interface. Platform: %s, Effective Name/ID: %s, IfIndex: %d, MAC: %s\n",
		runtime.GOOS, iface.Name, iface.GetIfIndex(), iface.GetMACAddress().String())

	return &Interface{Iface: iface}, nil
}

// --- Read, Write, Close methods ---

func (i *Interface) Read(b []byte) (int, error) {
	if i.Iface == nil {
		return 0, os.ErrInvalid
	}
	return i.Iface.Read(b)
}

func (i *Interface) Write(b []byte) (int, error) {
	if i.Iface == nil {
		return 0, os.ErrInvalid
	}
	return i.Iface.Write(b)
}

func (i *Interface) Close() error {
	if i.Iface == nil {
		return nil
	} // Or os.ErrInvalid?
	return i.Iface.Close()
}

// --- Accessor methods ---

// Name returns the effective name/identifier (GUID on Windows, name on Linux)
func (i *Interface) Name() string {
	if i.Iface == nil {
		return ""
	}
	return i.Iface.Name
}

func (i *Interface) HardwareAddr() net.HardwareAddr {
	if i.Iface == nil {
		return nil
	}
	// Always call the method on the Device; the platform-specific
	// implementation will handle retrieval (stored value or dynamic lookup).
	return i.Iface.GetMACAddress()
}

// GetIfIndex returns the interface index by calling the underlying Device method.
func (i *Interface) GetIfIndex() uint32 {
	if i.Iface == nil {
		return 0
	}
	// Always call the method on the Device.
	return i.Iface.GetIfIndex()
}

/*
// HardwareAddr returns the MAC address discovered during interface creation.
func (i *Interface) HardwareAddr() net.HardwareAddr {
	if i.Iface == nil {
		return nil
	}
	// On Windows, delegate to the stored MAC address in the Device struct
	if runtime.GOOS == "windows" {
		return i.Iface.GetMACAddress() // Use the stored MAC
	}

	// Linux implementation (remains the same, using net.InterfaceByName)
	iface, err := net.InterfaceByName(i.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: hardwareAddr (linux): could not get interface %s: %v\n", i.Name(), err)
		return nil
	}
	// Prefer 6-byte MAC if available
	if len(iface.HardwareAddr) >= 6 {
		macCopy := make(net.HardwareAddr, 6)
		copy(macCopy, iface.HardwareAddr[:6])
		return macCopy
	}
	// Return whatever was found otherwise
	return iface.HardwareAddr
}

// GetIfIndex returns the interface index discovered during creation (Windows)
// or dynamically looks it up (Linux). Returns 0 if not found/applicable.
func (i *Interface) GetIfIndex() uint32 {
	if i.Iface == nil {
		return 0
	}
	if runtime.GOOS == "windows" {
		// Delegate to the stored IfIndex in the Device struct
		return i.Iface.GetIfIndex()
	}
	// Linux implementation
	iface, err := net.InterfaceByName(i.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: GetIfIndex (linux): could not get interface %s: %v\n", i.Name(), err)
		return 0
	}
	return uint32(iface.Index)
}
*/
// --- Deadline methods ---

func (i *Interface) SetReadDeadline(t time.Time) error {
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetReadDeadline(t)
}

func (i *Interface) SetWriteDeadline(t time.Time) error {
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetWriteDeadline(t)
}

func (i *Interface) SetDeadline(t time.Time) error {
	// Delegate directly to underlying device method
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetDeadline(t)
}

// ConfigureInterface delegates to the platform-specific implementation.
// Defined in link_linux.go and link_windows.go.
// func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error

// IfUp provides a simpler way to bring the interface up with IP and default MTU.
// Defined in link_linux.go and link_windows.go.
// func (i *Interface) IfUp(ipCIDR string) error

// IfMac attempts to set the MAC address (mostly relevant/possible on Linux).
// Defined in link_linux.go and link_windows.go.
// func (i *Interface) IfMac(macAddr string) error

// SendGratuitousARP sends a gratuitous ARP reply for the given IP address
// using the MAC address of this interface.
// Defined in arp.go (which calls platform-specific helpers).
// func (i *Interface) SendGratuitousARP(vip net.IP) error

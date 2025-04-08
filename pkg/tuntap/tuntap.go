package tuntap

import (
	"fmt"
	"net"
	"os"
	"runtime" // Used only for initial logging msg
	"time"
)

// Interface is the primary structure for interacting with a TUN/TAP device.
// It wraps the lower-level, platform-specific Device object.
type Interface struct {
	// Iface holds the underlying platform-specific device state and methods.
	// We make it exported if external packages might need direct access,
	// otherwise it could be unexported. Let's keep it exported for now.
	Iface *Device
}

// NewInterface creates and initializes a new TUN/TAP interface based on the provided configuration.
// The caller is responsible for populating the config, especially config.MACAddress if desired on Windows.
func NewInterface(config Config) (*Interface, error) {
	// Validate config minimally
	if config.DevType != TAP {
		// Add TUN support later if needed and feasible
		return nil, fmt.Errorf("unsupported device type: %v (only TAP currently supported)", config.DevType)
	}
	if config.Name == "" && runtime.GOOS == "linux" {
		// Linux usually requires a name hint, though kernel assigns final one
		// Windows finds based on driver ComponentId, name is less critical here
		// We might want better name handling later.
		// return nil, fmt.Errorf("interface name must be provided for Linux")
		fmt.Fprintln(os.Stderr, "Warning: Interface name not provided in config, kernel will assign one (Linux).")

	}

	// Create calls the platform-specific implementation defined in device_*.go
	iface, err := Create(config) // Pass the caller-provided config
	if err != nil {
		// Add context to the error from Create
		return nil, fmt.Errorf("failed to create %s interface (name hint: %s): %w", config.DevType, config.Name, err)
	}

	// Log successful creation using the Device methods (which are platform-aware)
	logMsg := fmt.Sprintf("Successfully created interface. Platform: %s, Name/ID: %s", runtime.GOOS, iface.Name)
	if ifIndex := iface.GetIfIndex(); ifIndex != 0 {
		logMsg += fmt.Sprintf(", IfIndex: %d", ifIndex)
	}
	if mac := iface.GetMACAddress(); mac != nil {
		logMsg += fmt.Sprintf(", MAC: %s", mac.String())
	}
	fmt.Println(logMsg) // Use proper logging in real code

	return &Interface{Iface: iface}, nil
}

// --- Methods for *Interface (mostly delegating to *Device) ---

// Read reads a packet from the interface.
func (i *Interface) Read(b []byte) (int, error) {
	if i.Iface == nil {
		return 0, os.ErrInvalid // Or specific "interface closed" error
	}
	return i.Iface.Read(b)
}

// Write writes a packet to the interface.
func (i *Interface) Write(b []byte) (int, error) {
	if i.Iface == nil {
		return 0, os.ErrInvalid
	}
	return i.Iface.Write(b)
}

// Close closes the interface.
func (i *Interface) Close() error {
	if i.Iface == nil {
		return nil // Already closed
	}
	err := i.Iface.Close()
	i.Iface = nil // Prevent further use
	return err
}

// Name returns the effective name/identifier of the interface
// (e.g., "tap0" on Linux, GUID on Windows).
func (i *Interface) Name() string {
	if i.Iface == nil {
		return ""
	}
	return i.Iface.Name
}

// HardwareAddr returns the MAC address of the interface.
func (i *Interface) HardwareAddr() net.HardwareAddr {
	if i.Iface == nil {
		return nil
	}
	// Delegate directly to the Device method, which handles platform differences.
	return i.Iface.GetMACAddress()
}

// GetIfIndex returns the OS interface index.
func (i *Interface) GetIfIndex() uint32 {
	if i.Iface == nil {
		return 0
	}
	// Delegate directly to the Device method.
	return i.Iface.GetIfIndex()
}

// SetReadDeadline sets the read deadline.
func (i *Interface) SetReadDeadline(t time.Time) error {
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (i *Interface) SetWriteDeadline(t time.Time) error {
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetWriteDeadline(t)
}

// SetDeadline sets both read and write deadlines.
func (i *Interface) SetDeadline(t time.Time) error {
	if i.Iface == nil {
		return os.ErrInvalid
	}
	return i.Iface.SetDeadline(t)
}

// --- Signatures for methods implemented elsewhere ---

// ConfigureInterface applies network configuration (IP, MTU, etc.) to the interface.
// Implementation is in link_linux.go and link_windows.go.
// func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error

// IfUp is a convenience method to set IP/MTU and bring the interface up.
// Implementation is in link_linux.go and link_windows.go.
// func (i *Interface) IfUp(ipCIDR string) error

// IfMac attempts to set the MAC address (primarily Linux).
// Implementation is in link_linux.go and link_windows.go.
// func (i *Interface) IfMac(macAddr string) error

// SendGratuitousARP sends a gratuitous ARP reply using this interface's MAC.
// Implementation is in arp.go (calling helpers in arp_*.go).
// func (i *Interface) SendGratuitousARP(vip net.IP) error

//go:build linux

package tuntap

import (
	"fmt" // Needed for net.HardwareAddr type in Config if defined here
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Original Linux IOCTL constants
const (
	TUNSETIFF     = 0x400454ca
	TUNSETPERSIST = 0x400454cb
	TUNSETOWNER   = 0x400454cc
	TUNSETGROUP   = 0x400454ce
)

// Original Linux interface flags
const (
	IFF_TUN   = 0x0001
	IFF_TAP   = 0x0002
	IFF_NO_PI = 0x1000 // No packet information
)

// DeviceType definition (ensure consistency if not in common file)
type DeviceType int

const (
	TUN DeviceType = iota
	TAP
)

// Config struct - Includes MACAddress field for cross-platform consistency, though ignored here.
type Config struct {
	Name        string      // Name of the interface (e.g., "tun0", "tap0")
	DevType     DeviceType  // TUN or TAP
	MACAddress  string      // <<< ADDED for consistency, but ignored by Linux Create
	Persist     bool        // Device persists after the program exits if true
	Owner       int         // UID of the owner (or -1 for no change)
	Group       int         // GID of the owner (or -1 for no change)
	Permissions os.FileMode // File permissions for the device
}

// Device struct for Linux (doesn't need stored ifIndex/macAddr)
type Device struct {
	File    *os.File
	Name    string
	DevType DeviceType
	Config  Config
}

// --- ioctl helper ---
func ioctl(f int, request uintptr, argp uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(f), request, argp)
	if errno != 0 {
		return fmt.Errorf("ioctl failed with '%s'", errno)
	}
	return nil
}

// --- Original Linux Create function (slightly adapted) ---
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		return nil, fmt.Errorf("only TAP mode supported in this simplified cross-platform setup")
	}
	// Log ignored MACAddress on Linux
	if config.MACAddress != "" {
		fmt.Fprintf(os.Stderr, "Warning: Config option MACAddress (%s) is ignored on Linux (set via link config).\n", config.MACAddress)
	}

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	ifr, err := unix.NewIfreq(config.Name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("create ifreq: %w", err)
	}

	flags := uint16(unix.IFF_TAP | IFF_NO_PI) // TAP with no packet info
	ifr.SetUint16(flags)

	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("ioctl TUNSETIFF failed: %w", err)
	}
	actualName := ifr.Name()

	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("set nonblock: %w", err)
	}
	// Owner/Group/Persist logic... (keep original logic, maybe add warnings on failure)
	// ...

	file := os.NewFile(uintptr(fd), "/dev/net/tun/"+actualName)
	dev := &Device{
		File:    file,
		Name:    actualName,
		DevType: config.DevType,
		Config:  config,
	}
	return dev, nil
}

// --- Methods remain the same ---
func (d *Device) Read(b []byte) (int, error)         { return d.File.Read(b) }
func (d *Device) Write(b []byte) (int, error)        { return d.File.Write(b) }
func (d *Device) Close() error                       { return d.File.Close() }
func (d *Device) GetFd() int                         { return int(d.File.Fd()) }
func (d *Device) IsTUN() bool                        { return d.DevType == TUN }
func (d *Device) IsTAP() bool                        { return d.DevType == TAP }
func (d *Device) SetReadDeadline(t time.Time) error  { return d.File.SetReadDeadline(t) }
func (d *Device) SetWriteDeadline(t time.Time) error { return d.File.SetWriteDeadline(t) }
func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}
func (d *Device) GetIfIndex() uint32 {
	if d == nil || d.Name == "" {
		return 0
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		// Optional: Log this error
		// log.Printf("Warning: GetIfIndex (linux): could not get interface %s: %v", d.Name, err)
		return 0
	}
	return uint32(iface.Index)
}

// GetMACAddress looks up the MAC address dynamically on Linux.
// Returns nil if not found or error occurs.
func (d *Device) GetMACAddress() net.HardwareAddr {
	if d == nil || d.Name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		// Optional: Log this error
		// log.Printf("Warning: GetMACAddress (linux): could not get interface %s: %v", d.Name, err)
		return nil
	}
	// Prefer 6-byte MAC if available
	if len(iface.HardwareAddr) >= 6 {
		macCopy := make(net.HardwareAddr, 6)
		copy(macCopy, iface.HardwareAddr[:6])
		return macCopy
	}
	// Return nil if no address found
	if len(iface.HardwareAddr) == 0 {
		return nil
	}
	// Return whatever non-standard length was found otherwise
	return iface.HardwareAddr
}

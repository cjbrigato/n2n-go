//go:build linux

package tuntap

import (
	"fmt"
	"net"
	"os"

	// "time" // No longer needed

	"golang.org/x/sys/unix"
	// REMOVED: "golang.org/x/sys/windows"
)

// --- Linux IOCTL/Flags Constants ---
const (
	TUNSETIFF     = 0x400454ca
	TUNSETPERSIST = 0x400454cb
	TUNSETOWNER   = 0x400454cc
	TUNSETGROUP   = 0x400454ce
)
const (
	IFF_TUN   = 0x0001
	IFF_TAP   = 0x0002
	IFF_NO_PI = 0x1000 // No packet information
)

// NOTE: DeviceType, Config, Device structs are now defined in types.go

// --- ioctl helper ---
func ioctl(f int, request uintptr, argp uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(f), request, argp)
	if errno != 0 {
		return fmt.Errorf("ioctl failed with '%s'", errno)
	}
	return nil
}

// --- Linux Create function (Unchanged) ---
// ... (Creates and returns *Device, handle/ifIndex/macAddr fields are zero/nil) ...
func Create(config Config) (*Device, error) {
	// ... (implementation as before) ...
	if config.DevType != TAP {
		return nil, fmt.Errorf("only TAP supported")
	}
	/*if config.MACAddress != "" {
		fmt.Fprintf(os.Stderr, "Warning: Config MACAddress (%s) ignored on Linux.\n", config.MACAddress)
	}*/
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("linux: open /dev/net/tun: %w", err)
	}
	ifr, err := unix.NewIfreq(config.Name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("linux: create ifreq: %w", err)
	}
	flags := uint16(unix.IFF_TAP | IFF_NO_PI)
	ifr.SetUint16(flags)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("linux: ioctl TUNSETIFF: %w", err)
	}
	actualName := ifr.Name()
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("linux: set nonblock: %w", err)
	}
	// ... (Owner/Group/Persist logic) ...
	file := os.NewFile(uintptr(fd), "/dev/net/tun/"+actualName)
	dev := &Device{File: file, Name: actualName, DevType: config.DevType, Config: config /* Other fields zero */}
	return dev, nil
}

// --- Platform-specific implementations for Device methods ---

// GetIfIndex looks up dynamically on Linux.
func (d *Device) GetIfIndex() uint32 {
	if d == nil || d.Name == "" {
		return 0
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		return 0
	}
	return uint32(iface.Index)
}

// GetMACAddress looks up dynamically on Linux.
func (d *Device) GetMACAddress() net.HardwareAddr {
	if d == nil || d.Name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		return nil
	}
	if len(iface.HardwareAddr) >= 6 {
		macCopy := make(net.HardwareAddr, 6)
		copy(macCopy, iface.HardwareAddr[:6])
		return macCopy
	}
	if len(iface.HardwareAddr) == 0 {
		return nil
	}
	macCopy := make(net.HardwareAddr, len(iface.HardwareAddr))
	copy(macCopy, iface.HardwareAddr)
	return macCopy
}

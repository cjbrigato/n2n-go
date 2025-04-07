//go:build linux

package tuntap

import (
	"errors"
	"fmt"
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

type DeviceType int

const (
	TUN DeviceType = iota
	TAP
)

// Config remains the same, but Owner/Group/Persist might be Linux-specific
type Config struct {
	Name        string      // Name of the interface (e.g., "tun0", "tap0")
	DevType     DeviceType  // TUN or TAP
	Persist     bool        // Device persists after the program exits if true (Linux-specific)
	Owner       int         // UID of the owner (or -1 for no change) (Linux-specific)
	Group       int         // GID of the owner (or -1 for no change) (Linux-specific)
	Permissions os.FileMode // File permissions for the device (Linux-specific)
}

type Device struct {
	File    *os.File
	Name    string
	DevType DeviceType
	Config  Config
}

// Linux-specific ioctl wrapper
func ioctl(f int, request uintptr, argp uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(f), request, argp)
	if errno != 0 {
		return fmt.Errorf("ioctl failed with '%s'", errno)
	}
	return nil
}

// Original Linux Create function
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		// NOTE: This simple implementation only supports TAP for cross-platform consistency for now
		//       but the original Linux code could support TUN easily.
		return nil, fmt.Errorf("only TAP mode is currently supported in this cross-platform setup")
	}

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/net/tun: %w", err)
	}

	ifr, err := unix.NewIfreq(config.Name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to create ifreq: %w", err)
	}

	var flags uint16
	switch config.DevType {
	case TUN:
		flags = unix.IFF_TUN
	case TAP:
		flags = unix.IFF_TAP
	default:
		unix.Close(fd)
		return nil, errors.New("unknown device type")
	}
	flags |= IFF_NO_PI // Always use no packet info for TAP
	ifr.SetUint16(flags)

	// Set interface name and flags
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("ioctl TUNSETIFF failed: %w", err)
	}

	// Retrieve the actual interface name assigned by the kernel
	actualName := ifr.Name()

	// Set non-blocking I/O
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to set nonblock: %w", err)
	}

	// Set owner/group if specified (use root/root by default if not)
	owner := config.Owner
	if owner == -1 {
		owner = 0 // Default to root
	}
	if err = unix.IoctlSetInt(fd, unix.TUNSETOWNER, owner); err != nil {
		// Don't fail hard, maybe permissions are insufficient
		fmt.Fprintf(os.Stderr, "Warning: could not set TUNSETOWNER: %v\n", err)
	}

	group := config.Group
	if group == -1 {
		group = 0 // Default to root group
	}
	if err = unix.IoctlSetInt(fd, unix.TUNSETGROUP, group); err != nil {
		// Don't fail hard
		fmt.Fprintf(os.Stderr, "Warning: could not set TUNSETGROUP: %v\n", err)
	}

	if config.Persist {
		if err = ioctl(fd, TUNSETPERSIST, uintptr(1)); err != nil {
			// Don't fail hard
			fmt.Fprintf(os.Stderr, "Warning: could not set TUNSETPERSIST: %v\n", err)
		}
	}

	file := os.NewFile(uintptr(fd), "/dev/net/tun/"+actualName)
	dev := &Device{
		File:    file,
		Name:    actualName, // Use the name returned by the kernel
		DevType: config.DevType,
		Config:  config,
	}

	return dev, nil
}

// --- Methods remain the same, delegating to os.File ---

func (d *Device) Read(b []byte) (int, error) {
	return d.File.Read(b)
}

func (d *Device) Write(b []byte) (int, error) {
	return d.File.Write(b)
}

func (d *Device) Close() error {
	return d.File.Close()
}

func (d *Device) GetFd() int {
	return int(d.File.Fd())
}

func (d *Device) IsTUN() bool {
	return d.DevType == TUN
}

func (d *Device) IsTAP() bool {
	return d.DevType == TAP
}

func (d *Device) SetReadDeadline(t time.Time) error {
	return d.File.SetReadDeadline(t)
}

func (d *Device) SetWriteDeadline(t time.Time) error {
	return d.File.SetWriteDeadline(t)
}

func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}
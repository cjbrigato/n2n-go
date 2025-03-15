// Package tuntap provides a pure Go implementation for working with TUN/TAP devices on Linux.
package tuntap

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// Constants for the ioctl calls
	TUNSETIFF     = 0x400454ca
	TUNSETPERSIST = 0x400454cb
	TUNSETOWNER   = 0x400454cc
	TUNSETGROUP   = 0x400454ce

	// TUN/TAP device flags
	IFF_TUN   = 0x0001
	IFF_TAP   = 0x0002
	IFF_NO_PI = 0x1000 // No packet information
)

// DeviceType represents the type of virtual network device
type DeviceType int

const (
	// TUN device operates at the IP level (layer 3)
	TUN DeviceType = iota
	// TAP device operates at the Ethernet level (layer 2)
	TAP
)

// Config contains the configuration options for a TUN/TAP device
type Config struct {
	Name        string      // Name of the interface (e.g., "tun0", "tap0")
	DevType     DeviceType  // TUN or TAP
	Persist     bool        // Device persists after the program exits if true
	Owner       int         // UID of the owner (or -1 for no change)
	Group       int         // GID of the owner (or -1 for no change)
	Permissions os.FileMode // File permissions for the device
}

// Device represents a TUN/TAP network device
type Device struct {
	File    *os.File
	Name    string
	DevType DeviceType
	Config  Config
}

func ioctl(f int, request uint32, argp uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(f), uintptr(request), argp)
	if errno != 0 {
		return fmt.Errorf("ioctl failed with '%s'", errno)
	}
	return nil
}

// Create creates and configures a new TUN/TAP device with the given configuration
func Create(config Config) (*Device, error) {
	// Open the clone device
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	ifr, err := unix.NewIfreq(config.Name)
	if err != nil {
		return nil, err
	}

	// Set flags according to device type
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

	// Add IFF_NO_PI to exclude packet information
	flags |= IFF_NO_PI

	ifr.SetUint16(flags)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		return nil, err
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}

	if err = unix.IoctlSetInt(fd, unix.TUNSETOWNER, 0); err != nil {
		return nil, err
	}

	if err = unix.IoctlSetInt(fd, unix.TUNSETGROUP, 0); err != nil {
		return nil, err
	}

	if config.Persist {
		if err = ioctl(fd, unix.TUNSETPERSIST, uintptr(1)); err != nil {
			return nil, err
		}
	}

	// Create the device
	dev := &Device{
		File:    os.NewFile(uintptr(fd), "/dev/net/tun"),
		Name:    config.Name,
		DevType: config.DevType,
		Config:  config,
	}

	return dev, nil
}

// Read reads a packet from the TUN/TAP device
func (d *Device) Read(b []byte) (int, error) {
	return d.File.Read(b)
}

// Write writes a packet to the TUN/TAP device
func (d *Device) Write(b []byte) (int, error) {
	return d.File.Write(b)
}

// Close closes the TUN/TAP device
func (d *Device) Close() error {
	return d.File.Close()
}

// GetFd returns the file descriptor for the TUN/TAP device
func (d *Device) GetFd() int {
	return int(d.File.Fd())
}

// IsTUN returns true if the device is a TUN device
func (d *Device) IsTUN() bool {
	return d.DevType == TUN
}

// IsTAP returns true if the device is a TAP device
func (d *Device) IsTAP() bool {
	return d.DevType == TAP
}

func (d *Device) SetReadDeadline(t time.Time) error {
	return d.File.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (d *Device) SetWriteDeadline(t time.Time) error {
	return d.File.SetWriteDeadline(t)
}

// SetDeadline sets the read and write deadlines for the device.
func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}

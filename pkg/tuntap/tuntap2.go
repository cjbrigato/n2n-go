// Package tuntap provides a pure Go implementation for working with TUN/TAP devices on Linux.
package tuntap

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
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

// ifReq is used for ioctl calls to configure the TUN/TAP device
type ifReq struct {
	Name  [16]byte
	Flags uint16
	pad   [22]byte // Padding to match struct ifreq in Linux
}

// Create creates and configures a new TUN/TAP device with the given configuration
func Create(config Config) (*Device, error) {
	// Open the clone device
	file, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/net/tun: %w", err)
	}

	// Prepare the ioctl request
	var req ifReq
	if len(config.Name) > 15 {
		return nil, errors.New("interface name too long")
	}
	copy(req.Name[:], config.Name)

	// Set flags according to device type
	var flags uint16
	switch config.DevType {
	case TUN:
		flags = IFF_TUN
	case TAP:
		flags = IFF_TAP
	default:
		file.Close()
		return nil, errors.New("unknown device type")
	}
	// Add IFF_NO_PI to exclude packet information
	flags |= IFF_NO_PI
	req.Flags = flags

	// Configure the TUN/TAP device through ioctl
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		file.Close()
		return nil, errno
	}

	// Extract the actual device name assigned by the kernel
	deviceName := string(req.Name[:])
	for i := 0; i < len(deviceName); i++ {
		if deviceName[i] == 0 {
			deviceName = deviceName[:i]
			break
		}
	}

	// Set additional options if specified
	if config.Persist {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(TUNSETPERSIST), uintptr(1))
		if errno != 0 {
			file.Close()
			return nil, errno
		}
	}

	if config.Owner != -1 {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(TUNSETOWNER), uintptr(config.Owner))
		if errno != 0 {
			file.Close()
			return nil, errno
		}
	}

	if config.Group != -1 {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(TUNSETGROUP), uintptr(config.Group))
		if errno != 0 {
			file.Close()
			return nil, errno
		}
	}

	// Create the device
	dev := &Device{
		File:    file,
		Name:    deviceName,
		DevType: config.DevType,
		Config:  config,
	}

	// Set non-blocking mode by default
	if err := dev.SetNonblocking(true); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to set non-blocking mode: %w", err)
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

// SetNonblocking sets the device file to non-blocking mode
func (d *Device) SetNonblocking(nonblocking bool) error {
	var flag int
	if nonblocking {
		flag = syscall.O_NONBLOCK
	} else {
		flag = 0
	}
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, d.File.Fd(), syscall.F_SETFL, uintptr(flag))
	if errno != 0 {
		return errno
	}
	return nil
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

//go:build netbsd

package tuntap

import (
	"errors"
	"fmt"
	"n2n-go/pkg/log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall" // Use syscall for constants like O_RDWR

	"golang.org/x/sys/unix" // Use unix for functions like Open, SetNonblock
)

// NOTE: DeviceType, Config, Device structs are defined in types.go

// Create opens an existing TAP device on NetBSD.
// NetBSD typically requires the TAP device to be created beforehand,
// e.g., using 'ifconfig tapN create'.
// It will try to open /dev/tap0 through /dev/tap15 unless a specific name like "tapN" is provided.
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		return nil, fmt.Errorf("netbsd: only TAP devices are supported")
	}

	var devName string
	var fd int
	var err error

	// Check if a specific tap device name is provided
	if config.Name != "" && filepath.Dir(config.Name) == "." && strings.HasPrefix(config.Name, "tap") {
		devPath := filepath.Join("/dev", config.Name)
		log.Printf("Attempting to open specified TAP device: %s", devPath)
		// Use O_RDWR from syscall as unix package might not have it directly aliased on all GOOS
		fd, err = unix.Open(devPath, syscall.O_RDWR, 0)
		if err == nil {
			devName = config.Name
		} else {
			log.Printf("Warning: Failed to open specified device %s: %v. Falling back to scanning.", devPath, err)
			// Fall through to scanning if specified device fails
		}
	}

	// If no specific name worked or none was given, scan /dev/tap[0-15]
	if devName == "" {
		for i := 0; i < 16; i++ {
			devPath := fmt.Sprintf("/dev/tap%d", i)
			log.Printf("Attempting to open TAP device: %s", devPath)
			fd, err = unix.Open(devPath, syscall.O_RDWR, 0)
			if err == nil {
				devName = fmt.Sprintf("tap%d", i)
				log.Printf("Successfully opened TAP device: %s", devPath)
				break
			}
			// If error is EBUSY or EPERM, it exists but we can't use it. If ENOENT, it doesn't exist.
			// Use unix constants for error checking.
			if !errors.Is(err, unix.ENOENT) {
				log.Printf("Info: Could not open %s: %v", devPath, err)
			}
		}
	}

	if devName == "" || err != nil {
		// Provide a more specific error message for NetBSD
		return nil, fmt.Errorf("netbsd: could not open any /dev/tap[0-15] device. Ensure TAP device exists (e.g., 'ifconfig tapN create') and permissions are correct. Last error: %w", err)
	}

	// Set to non-blocking mode
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("netbsd: failed to set non-blocking mode for %s: %w", devName, err)
	}

	// Create an os.File wrapper for the file descriptor
	file := os.NewFile(uintptr(fd), "/dev/"+devName)

	// Create the Device struct
	dev := &Device{
		devIo:   file, // Use the os.File as the DeviceIO
		Name:    devName,
		DevType: config.DevType,
		Config:  config,
		// ifIndex and macAddr will be looked up dynamically via methods
	}

	// Log ignored options (similar to Darwin)
	if config.MACAddress != "" {
		fmt.Fprintf(os.Stderr, "Warning: Config.MACAddress (%s) ignored on NetBSD. Use ifconfig manually or ConfigureInterface.\n", config.MACAddress)
	}
	if config.Persist || config.Owner != 0 || config.Group != 0 {
		fmt.Fprintln(os.Stderr, "Warning: Config.Persist, Config.Owner, Config.Group are ignored on NetBSD.")
	}

	log.Printf("Successfully created TAP device %s", devName)
	return dev, nil
}

// --- Platform-specific implementations for Device methods ---

// GetIfIndex looks up the interface index dynamically using net.InterfaceByName.
func (d *Device) GetIfIndex() uint32 {
	if d == nil || d.Name == "" {
		return 0
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		log.Printf("Warning: GetIfIndex failed for %s: %v", d.Name, err)
		return 0
	}
	return uint32(iface.Index)
}

// GetMACAddress looks up the MAC address dynamically using net.InterfaceByName.
func (d *Device) GetMACAddress() net.HardwareAddr {
	if d == nil || d.Name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(d.Name)
	if err != nil {
		log.Printf("Warning: GetMACAddress failed for %s: %v", d.Name, err)
		return nil
	}
	// Return a copy to prevent modification of the original slice
	if len(iface.HardwareAddr) > 0 {
		macCopy := make(net.HardwareAddr, len(iface.HardwareAddr))
		copy(macCopy, iface.HardwareAddr)
		return macCopy
	}
	return nil
}

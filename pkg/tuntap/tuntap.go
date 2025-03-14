// Package tuntap provides a cross‑platform interface for working with TUN/TAP devices.
// It uses the github.com/songgao/water package to create and manage virtual network interfaces.
package tuntap

import (
	"fmt"
	"net"
	"time"
)

// Interface wraps a TAP interface.
type Interface struct {
	//ifce *water.Interface
	Iface *Device
}

// NewInterface creates a new TAP interface with the specified name.
// mode must be "tap" to indicate a TAP device.
func NewInterface(name, mode string) (*Interface, error) {
	if mode != "tap" {
		return nil, fmt.Errorf("unsupported mode: %s (must be 'tap')", mode)
	}

	config := Config{
		Name:        name,
		DevType:     TAP,
		Persist:     false,
		Owner:       -1,
		Group:       -1,
		Permissions: 0666,
	}

	/*config := water.Config{
		DeviceType: water.TAP,
	}
	config.Name = name
	ifce, err := water.New(config)*/

	iface, err := Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %v", name, err)
	}
	return &Interface{Iface: iface}, nil //ifce: ifce}, nil
}

// Read reads data from the TAP interface.
func (i *Interface) Read(b []byte) (int, error) {
	return i.Iface.Read(b)
}

// Write writes data to the TAP interface.
func (i *Interface) Write(b []byte) (int, error) {
	return i.Iface.Write(b)
}

// Close closes the TAP interface.
func (i *Interface) Close() error {
	return i.Iface.Close()
}

// Name returns the name of the TAP interface.
func (i *Interface) Name() string {
	return i.Iface.Name
}

func (i *Interface) HardwareAddr() net.HardwareAddr {
	addr, _ := i.hardwareAddr()
	return addr
}

func (i *Interface) SetReadDeadline(t time.Time) error {
	return i.Iface.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (i *Interface) SetWriteDeadline(t time.Time) error {
	return i.Iface.SetWriteDeadline(t)
}

// SetDeadline sets the read and write deadlines for the interface.
func (i *Interface) SetDeadline(t time.Time) error {
	if err := i.SetReadDeadline(t); err != nil {
		return err
	}
	return i.SetWriteDeadline(t)
}

// hardwareAddr retrieves the MAC address of the TAP interface using its name.
func (i *Interface) hardwareAddr() (net.HardwareAddr, error) {
	iface, err := net.InterfaceByName(i.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %v", i.Name(), err)
	}
	if iface.HardwareAddr == nil || len(iface.HardwareAddr) < 6 {
		return nil, fmt.Errorf("no valid MAC address found on interface %s", i.Name())
	}
	// Return the first 6 bytes.
	return iface.HardwareAddr[:6], nil
}

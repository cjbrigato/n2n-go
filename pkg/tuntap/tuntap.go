// Package tuntap provides a cross‑platform interface for working with TUN/TAP devices.
// It uses the github.com/songgao/water package to create and manage virtual network interfaces.
package tuntap

import (
	"fmt"

	"github.com/cjbrigato/water"
)

// Interface wraps a TAP interface.
type Interface struct {
	ifce *water.Interface
}

// NewInterface creates a new TAP interface with the specified name.
// mode must be "tap" to indicate a TAP device.
func NewInterface(name, mode string) (*Interface, error) {
	if mode != "tap" {
		return nil, fmt.Errorf("unsupported mode: %s (must be 'tap')", mode)
	}
	config := water.Config{
		DeviceType: water.TAP,
	}
	config.Name = name
	ifce, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %v", name, err)
	}
	return &Interface{ifce: ifce}, nil
}

// Read reads data from the TAP interface.
func (i *Interface) Read(b []byte) (int, error) {
	return i.ifce.Read(b)
}

// Write writes data to the TAP interface.
func (i *Interface) Write(b []byte) (int, error) {
	return i.ifce.Write(b)
}

// Close closes the TAP interface.
func (i *Interface) Close() error {
	return i.ifce.Close()
}

// Name returns the name of the TAP interface.
func (i *Interface) Name() string {
	return i.ifce.Name()
}

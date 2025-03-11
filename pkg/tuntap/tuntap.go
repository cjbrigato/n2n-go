// Package tuntap provides a cross‑platform interface for working with TUN/TAP devices.
// It uses the github.com/songgao/water package to create and manage virtual network interfaces.
package tuntap

import (
	"fmt"

	"github.com/cjbrigato/water"
)

// Interface wraps a TUN/TAP interface.
type Interface struct {
	ifce *water.Interface
}

// NewInterface creates a new TUN/TAP interface with the specified name and mode.
// The mode parameter can be "tun" or "tap". If an unrecognized mode is provided, "tun" is used by default.
func NewInterface(name, mode string) (*Interface, error) {
	config := water.Config{}
	switch mode {
	case "tap":
		config.DeviceType = water.TAP
	default:
		config.DeviceType = water.TUN
	}
	//config.Name = name
	ifce, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s interface %s: %v", mode, name, err)
	}
	return &Interface{ifce: ifce}, nil
}

// Read reads data from the TUN/TAP interface.
func (i *Interface) Read(b []byte) (int, error) {
	return i.ifce.Read(b)
}

// Write writes data to the TUN/TAP interface.
func (i *Interface) Write(b []byte) (int, error) {
	return i.ifce.Write(b)
}

// Close closes the TUN/TAP interface.
func (i *Interface) Close() error {
	return i.ifce.Close()
}

// Name returns the name of the interface.
func (i *Interface) Name() string {
	return i.ifce.Name()
}

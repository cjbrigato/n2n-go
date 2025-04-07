//go:build windows

package tuntap

import (
	"fmt"
)

// sendRawFrame writes the Ethernet frame directly to the TAP device on Windows.
func (i *Interface) sendRawFrame(frame []byte) error {
	n, err := i.Write(frame) // Use the Write method of the *tuntap.Interface receiver
	if err != nil {
		return fmt.Errorf("windows: failed to write frame to TAP device %s: %w", i.Name(), err)
	}
	if n != len(frame) {
		return fmt.Errorf("windows: short write sending frame to TAP device %s (%d/%d bytes)", i.Name(), n, len(frame))
	}
	return nil
}

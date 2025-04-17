//go:build darwin

package tuntap

import (
	"errors"
	"fmt"
	"os" // Needed for os.ErrInvalid check
)

// sendRawFrame writes the Ethernet frame directly to the TAP device file descriptor on Darwin.
// This relies on the underlying os.File (*Device.devIo) being the correct TAP device.
func (i *Interface) sendRawFrame(frame []byte) error {
	if i.Iface == nil || i.Iface.devIo == nil {
		return fmt.Errorf("darwin: interface or underlying device IO is nil")
	}

	// Write directly to the file descriptor associated with the TAP device
	n, err := i.Write(frame) // Use the Write method of the *tuntap.Interface -> *Device -> devIo
	if err != nil {
		// Check if the error is because the interface was closed
		if errors.Is(err, os.ErrInvalid) || errors.Is(err, os.ErrClosed) { // Check common closed errors
			return fmt.Errorf("darwin: failed to write frame to TAP device %s (interface closed): %w", i.Name(), err)
		}
		return fmt.Errorf("darwin: failed to write frame to TAP device %s: %w", i.Name(), err)
	}
	if n != len(frame) {
		return fmt.Errorf("darwin: short write sending frame to TAP device %s (%d/%d bytes)", i.Name(), n, len(frame))
	}
	return nil
}

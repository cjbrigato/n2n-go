package util

import (
	"fmt"
	"os/exec"
)

// ConfigureInterface brings up the TAP interface and assigns the given IP address.
func IfUp(ifName, ipAddr string) error {

	// Bring the interface up
	cmdUp := exec.Command("ip", "link", "set", "dev", ifName, "up")
	if err := cmdUp.Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Assign IP address
	cmdAddr := exec.Command("ip", "addr", "add", ipAddr, "dev", ifName)
	if err := cmdAddr.Run(); err != nil {
		return fmt.Errorf("failed to assign IP address: %w", err)
	}

	cmdMTU := exec.Command("ip", "link", "set", "dev", ifName, "mtu", "1420")
	if err := cmdMTU.Run(); err != nil {
		return fmt.Errorf("failed to assign IP address: %w", err)
	}

	return nil
}

package tuntap

import (
	"testing"
)

func TestNewInterface(t *testing.T) {
	// Attempt to create a TUN interface named "n2n0".
	iface, err := NewInterface("n2n0", "tun")
	if err != nil {
		// On systems without TUN/TAP support, skip the test.
		t.Skip("Skipping TUN/TAP interface test:", err)
	}
	if iface == nil {
		t.Fatal("expected non‑nil interface")
	}
	if iface.Name() == "" {
		t.Fatal("interface name should not be empty")
	}
	// Test read/write functionality would normally require interacting with the system.
	// Here we just close the interface.
	if err := iface.Close(); err != nil {
		t.Fatalf("error closing interface: %v", err)
	}
}

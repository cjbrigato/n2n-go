package trafficfilter

import (
	"net"
	"testing"
)

func TestFilterPacket(t *testing.T) {
	f := NewFilter()

	// Define some IP addresses for testing.
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("10.0.0.5")
	otherIP := net.ParseIP("192.168.1.101")

	// Add a rule that blocks packets from srcIP.
	f.AddRule(Rule{
		SourceIP: srcIP,
		Allow:    false,
	})

	// Packet from srcIP should be blocked.
	if f.FilterPacket(srcIP, dstIP, ProtocolIPv4) {
		t.Errorf("Packet from %v should be blocked", srcIP)
	}

	// Packet from otherIP should be allowed (no matching rule).
	if !f.FilterPacket(otherIP, dstIP, ProtocolIPv4) {
		t.Errorf("Packet from %v should be allowed", otherIP)
	}

	// Add a rule to explicitly allow packets to dstIP.
	f.AddRule(Rule{
		DestinationIP: dstIP,
		Allow:         true,
	})

	// Even though srcIP is blocked by the first rule, the order matters.
	// Here, since rules are evaluated in order, the first matching rule (block srcIP) applies.
	if f.FilterPacket(srcIP, dstIP, ProtocolIPv4) {
		t.Errorf("Packet from %v to %v should still be blocked", srcIP, dstIP)
	}

	// Clear rules and test default allow.
	f.ClearRules()
	if !f.FilterPacket(srcIP, dstIP, ProtocolIPv4) {
		t.Errorf("With no rules, packet should be allowed")
	}
}

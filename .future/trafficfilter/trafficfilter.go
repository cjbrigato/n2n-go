// Package trafficfilter provides functionality to filter network traffic
// based on configurable rules. This is a rewrite of the n2n network_traffic_filter.c/h functionality.
package trafficfilter

import (
	"net"
	"sync"
)

// Protocol represents a simplified network protocol.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolIPv4
	ProtocolIPv6
	// Extend with more protocols as needed.
)

// Rule defines a single filtering rule.
type Rule struct {
	// If SourceIP is non-nil, only packets from this IP will match.
	SourceIP net.IP
	// If DestinationIP is non-nil, only packets to this IP will match.
	DestinationIP net.IP
	// Protocol to filter; if zero, match any.
	Protocol Protocol
	// Allow indicates whether matching packets should be allowed (true) or dropped (false).
	Allow bool
}

// Filter holds a set of filtering rules.
type Filter struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewFilter creates a new Filter.
func NewFilter() *Filter {
	return &Filter{
		rules: make([]Rule, 0),
	}
}

// AddRule adds a new filtering rule to the filter.
func (f *Filter) AddRule(rule Rule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, rule)
}

// ClearRules removes all filtering rules.
func (f *Filter) ClearRules() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = f.rules[:0]
}

// FilterPacket inspects packet header fields (source IP, destination IP, protocol)
// and returns true if the packet should be allowed, or false if it should be dropped.
// If no rule matches, the packet is allowed by default.
func (f *Filter) FilterPacket(src, dst net.IP, proto Protocol) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Evaluate rules in order. If any rule matches, return its Allow value.
	for _, rule := range f.rules {
		// Match source IP if specified.
		if rule.SourceIP != nil && !rule.SourceIP.Equal(src) {
			continue
		}
		// Match destination IP if specified.
		if rule.DestinationIP != nil && !rule.DestinationIP.Equal(dst) {
			continue
		}
		// Match protocol if specified (non-zero).
		if rule.Protocol != 0 && rule.Protocol != proto {
			continue
		}
		return rule.Allow
	}

	// Default action: allow the packet.
	return true
}

// Package management provides functionality to manage IoT edge registrations,
// including registering/updating edges, allocating virtual IP addresses, and
// cleaning up stale edge records. This is inspired by n2n's management.c, edge_management.c,
// and related utilities.
package management

import (
	"crypto/sha256"
	"encoding/binary"
	"net"
	"sync"
	"time"
)

// EdgeRecord represents the management information for a registered edge.
type EdgeRecord struct {
	ID            string    // unique identifier (e.g., MAC address or description)
	VirtualIP     net.IP    // assigned virtual IP address
	PublicIP      net.IP    // real IP address as seen by the supernode
	Port          int       // UDP port number from which the edge communicates
	Community     string    // community (network) name the edge belongs to
	LastHeartbeat time.Time // timestamp of the last registration or heartbeat
}

// Manager maintains the state of registered edges.
type Manager struct {
	mu          sync.RWMutex
	edges       map[string]*EdgeRecord // keyed by edge ID
	BaseNetwork net.IP                 // base network (e.g. 10.0.0.0)
	Mask        net.IPMask             // network mask (e.g. 255.0.0.0 for /8)
}

// NewManager creates a new Manager given a base network and mask.
// For example, NewManager(net.ParseIP("10.0.0.0"), net.CIDRMask(8, 32)).
func NewManager(baseNetwork net.IP, mask net.IPMask) *Manager {
	return &Manager{
		edges:       make(map[string]*EdgeRecord),
		BaseNetwork: baseNetwork.To4(), // assume IPv4
		Mask:        mask,
	}
}

// RegisterEdge creates a new edge record or updates an existing one.
// It allocates a virtual IP for the edge (if new) based on a hash of the edge ID.
func (m *Manager) RegisterEdge(id string, publicIP net.IP, port int, community string) *EdgeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()

	if edge, exists := m.edges[id]; exists {
		edge.PublicIP = publicIP
		edge.Port = port
		edge.Community = community
		edge.LastHeartbeat = time.Now()
		return edge
	}

	// Allocate a virtual IP address based on the edge ID.
	vip := m.allocateVirtualIP(id)
	edge := &EdgeRecord{
		ID:            id,
		VirtualIP:     vip,
		PublicIP:      publicIP,
		Port:          port,
		Community:     community,
		LastHeartbeat: time.Now(),
	}
	m.edges[id] = edge
	return edge
}

// UpdateHeartbeat refreshes the heartbeat timestamp for an edge.
func (m *Manager) UpdateHeartbeat(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if edge, ok := m.edges[id]; ok {
		edge.LastHeartbeat = time.Now()
	}
}

// RemoveEdge deletes an edge record by its ID.
func (m *Manager) RemoveEdge(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.edges, id)
}

// RemoveStaleEdges removes edges that haven't updated their heartbeat within the expiry duration.
func (m *Manager) RemoveStaleEdges(expiry time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for id, edge := range m.edges {
		if now.Sub(edge.LastHeartbeat) > expiry {
			delete(m.edges, id)
		}
	}
}

// ListEdges returns a slice of all registered edges.
func (m *Manager) ListEdges() []*EdgeRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*EdgeRecord
	for _, edge := range m.edges {
		list = append(list, edge)
	}
	return list
}

// allocateVirtualIP calculates a virtual IP for an edge based on its ID.
// It hashes the edge ID and uses the lower bits as an offset to add to the base network.
// Note: This is a simple algorithm and does not check for collisions.
func (m *Manager) allocateVirtualIP(id string) net.IP {
	// Compute a SHA-256 hash of the edge ID.
	h := sha256.Sum256([]byte(id))
	// Use the lower 24 bits of the hash as an offset.
	offset := binary.BigEndian.Uint32(h[:4]) & 0x00FFFFFF

	// Get the base network as a uint32.
	base := binary.BigEndian.Uint32(m.BaseNetwork)
	// Combine the base network with the offset.
	vipUint := base | offset

	// Convert the uint32 back to an IP.
	vip := make(net.IP, 4)
	binary.BigEndian.PutUint32(vip, vipUint)
	return vip
}

// GetEdge retrieves an edge by its ID.
func (m *Manager) GetEdge(id string) (*EdgeRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	edge, ok := m.edges[id]
	return edge, ok
}

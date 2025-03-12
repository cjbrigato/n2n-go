// Package portmap provides functionality for managing port mappings,
// similar to n2n_port_mapping.c/h in the original n2n repository.
// It allows adding, removing, querying, and listing port mappings.
package portmap

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mapping represents a port mapping from an external port to an internal port.
type Mapping struct {
	ExternalPort int
	InternalPort int
	// Optionally, you can add a timestamp for last update.
	LastUpdated time.Time
}

// Manager manages port mappings.
type Manager struct {
	// mappings is a map keyed by external port.
	mappings map[int]*Mapping
	mu       sync.RWMutex
}

// NewManager creates a new port mapping manager.
func NewManager() *Manager {
	return &Manager{
		mappings: make(map[int]*Mapping),
	}
}

// ParseMapping parses a mapping string of the form "ext:int" (e.g., "80:8080")
// and returns a Mapping.
func ParseMapping(s string) (*Mapping, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return nil, errors.New("invalid mapping format; expected 'external:internal'")
	}
	ext, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid external port: %v", err)
	}
	intPort, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid internal port: %v", err)
	}
	if ext <= 0 || ext > 65535 || intPort <= 0 || intPort > 65535 {
		return nil, errors.New("port numbers must be between 1 and 65535")
	}
	return &Mapping{
		ExternalPort: ext,
		InternalPort: intPort,
		LastUpdated:  time.Now(),
	}, nil
}

// AddMapping adds a new port mapping to the manager. If a mapping for the same external
// port exists, it is updated.
func (m *Manager) AddMapping(mapping *Mapping) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mappingUpdate(mapping)
}

// RemoveMapping removes the mapping for the given external port.
func (m *Manager) RemoveMapping(externalPort int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mappings, externalPort)
}

// GetMapping retrieves the mapping for a given external port.
func (m *Manager) GetMapping(externalPort int) (*Mapping, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mapping, ok := m.mappings[externalPort]
	return mapping, ok
}

// ListMappings returns a copy of all current mappings.
func (m *Manager) ListMappings() []*Mapping {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*Mapping
	for _, mapping := range m.mappings {
		list = append(list, mapping)
	}
	return list
}

// mappingUpdate inserts or updates a mapping in the manager.
func (m *Manager) mappingUpdate(mapping *Mapping) {
	// Update the timestamp.
	mapping.LastUpdated = time.Now()
	m.mappings[mapping.ExternalPort] = mapping
}

// RefreshMappings periodically removes stale mappings that haven't been updated within the given duration.
func (m *Manager) RefreshMappings(expiry time.Duration, stop chan struct{}) {
	ticker := time.NewTicker(expiry / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			for port, mapping := range m.mappings {
				if now.Sub(mapping.LastUpdated) > expiry {
					delete(m.mappings, port)
				}
			}
			m.mu.Unlock()
		case <-stop:
			return
		}
	}
}

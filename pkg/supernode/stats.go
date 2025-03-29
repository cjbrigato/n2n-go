package supernode

import (
	"sync/atomic"
	"time"
)

// SupernodeStats holds runtime statistics
type SupernodeStats struct {
	PacketsProcessed       atomic.Uint64
	PacketsForwarded       atomic.Uint64
	PacketsDropped         atomic.Uint64
	EdgesRegistered        atomic.Uint64
	EdgesUnregistered      atomic.Uint64
	HeartbeatsReceived     atomic.Uint64
	CompactPacketsRecv     atomic.Uint64
	LegacyPacketsRecv      atomic.Uint64
	HashCollisionsDetected atomic.Uint64
	LastCleanupTime        time.Time
	LastCleanupEdges       int
}

// GetStats returns a copy of the current statistics
func (s *Supernode) GetStats() SupernodeStats {
	return SupernodeStats{
		PacketsProcessed:       atomic.Uint64{},
		PacketsForwarded:       atomic.Uint64{},
		PacketsDropped:         atomic.Uint64{},
		EdgesRegistered:        atomic.Uint64{},
		EdgesUnregistered:      atomic.Uint64{},
		HeartbeatsReceived:     atomic.Uint64{},
		CompactPacketsRecv:     atomic.Uint64{},
		LegacyPacketsRecv:      atomic.Uint64{},
		HashCollisionsDetected: atomic.Uint64{},
		LastCleanupTime:        s.stats.LastCleanupTime,
		LastCleanupEdges:       s.stats.LastCleanupEdges,
	}
}

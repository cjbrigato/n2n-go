package supernode

import (
	"log"
	"time"
)

// CleanupStaleEdges removes edges that haven't sent a heartbeat within the expiry period
func (s *Supernode) CleanupStaleEdges(expiry time.Duration) {

	now := time.Now()
	stales := make(map[*Community][]string)
	var cms []*Community
	s.comMu.RLock()
	for _, cm := range s.communities {
		cms = append(cms, cm)
	}
	s.comMu.RUnlock()
	var totalCleanUp int
	for _, cm := range cms {
		stales[cm] = cm.GetStaleEdgeIDs(expiry)
		totalCleanUp += len(stales[cm])
	}

	for k, vv := range stales {
		for _, id := range vv {
			if k.Unregister(id) {
				log.Printf("Supernode: Removed stale edge %s from community %s", id, k.Name())
				s.stats.EdgesUnregistered.Add(1)
				s.onEdgeUnregistered(k, id)
			}
		}
	}
	// Update statistics
	s.stats.LastCleanupTime = now
	s.stats.LastCleanupEdges = totalCleanUp
}

// cleanupRoutine periodically cleans up stale edges
func (s *Supernode) cleanupRoutine() {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.CleanupStaleEdges(s.config.ExpiryDuration)
		case <-s.shutdownCh:
			return
		}
	}
}

package management

import (
	"net"
	"testing"
	"time"
)

func TestRegisterAndList(t *testing.T) {
	baseIP := net.ParseIP("10.0.0.0")
	mask := net.CIDRMask(8, 32)
	mgr := NewManager(baseIP, mask)

	edge1 := mgr.RegisterEdge("edge1", net.ParseIP("192.168.1.10"), 5000, "community1")
	edge2 := mgr.RegisterEdge("edge2", net.ParseIP("192.168.1.11"), 5001, "community1")

	if edge1 == nil || edge2 == nil {
		t.Fatal("Expected edges to be registered")
	}

	edges := mgr.ListEdges()
	if len(edges) != 2 {
		t.Fatalf("Expected 2 edges, got %d", len(edges))
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	baseIP := net.ParseIP("10.0.0.0")
	mask := net.CIDRMask(8, 32)
	mgr := NewManager(baseIP, mask)

	mgr.RegisterEdge("edge1", net.ParseIP("192.168.1.10"), 5000, "community1")
	edgeBefore, ok := mgr.GetEdge("edge1")
	if !ok {
		t.Fatal("Edge edge1 not found")
	}
	heartbeatBefore := edgeBefore.LastHeartbeat

	time.Sleep(10 * time.Millisecond)
	mgr.UpdateHeartbeat("edge1")

	edgeAfter, ok := mgr.GetEdge("edge1")
	if !ok {
		t.Fatal("Edge edge1 not found after update")
	}
	if !edgeAfter.LastHeartbeat.After(heartbeatBefore) {
		t.Errorf("Heartbeat was not updated")
	}
}

func TestRemoveStaleEdges(t *testing.T) {
	baseIP := net.ParseIP("10.0.0.0")
	mask := net.CIDRMask(8, 32)
	mgr := NewManager(baseIP, mask)

	mgr.RegisterEdge("edge1", net.ParseIP("192.168.1.10"), 5000, "community1")
	mgr.RegisterEdge("edge2", net.ParseIP("192.168.1.11"), 5001, "community1")

	// Wait longer than the expiry duration.
	time.Sleep(60 * time.Millisecond)
	mgr.RemoveStaleEdges(50 * time.Millisecond)

	edges := mgr.ListEdges()
	if len(edges) != 0 {
		t.Errorf("Expected all stale edges to be removed, got %d", len(edges))
	}
}

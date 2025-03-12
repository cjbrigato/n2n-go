package portmap

import (
	"testing"
	"time"
)

func TestParseMapping(t *testing.T) {
	m, err := ParseMapping("80:8080")
	if err != nil {
		t.Fatalf("ParseMapping failed: %v", err)
	}
	if m.ExternalPort != 80 || m.InternalPort != 8080 {
		t.Errorf("Expected mapping 80:8080, got %d:%d", m.ExternalPort, m.InternalPort)
	}

	_, err = ParseMapping("invalid")
	if err == nil {
		t.Errorf("Expected error for invalid mapping format")
	}
}

func TestManagerAddGetRemove(t *testing.T) {
	mgr := NewManager()
	mapping, _ := ParseMapping("443:8443")
	mgr.AddMapping(mapping)

	if got, ok := mgr.GetMapping(443); !ok {
		t.Errorf("Mapping for external port 443 not found")
	} else if got.InternalPort != 8443 {
		t.Errorf("Expected internal port 8443, got %d", got.InternalPort)
	}

	mgr.RemoveMapping(443)
	if _, ok := mgr.GetMapping(443); ok {
		t.Errorf("Mapping for external port 443 should have been removed")
	}
}

func TestRefreshMappings(t *testing.T) {
	mgr := NewManager()
	mapping, _ := ParseMapping("1000:2000")
	mgr.AddMapping(mapping)

	stop := make(chan struct{})
	go mgr.RefreshMappings(100*time.Millisecond, stop)
	// Wait for longer than expiry.
	time.Sleep(150 * time.Millisecond)
	close(stop)

	if _, ok := mgr.GetMapping(1000); ok {
		t.Errorf("Mapping for external port 1000 should have expired and been removed")
	}
}

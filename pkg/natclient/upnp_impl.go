package natclient

import (
	"context"
	"fmt"
	"n2n-go/pkg/log"
	"strings"
	"time" // Needed for getLocalIP fallback

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// UPnPClient encapsulates IGD (Internet Gateway Device) functionality
type UPnPClient struct {
	gateway     interface{}       // Holds the specific goupnp client
	GatewayType string            // e.g., "IGDv1-IP1"
	LocalIP     string            // Cached local IP
	ExternalIP  string            // Cached external IP
	MappedPorts map[uint16]string // protocol (UDP/TCP) mapped by external port
}

// Compile-time check to ensure UPnPClient implements NATClient
var _ NATClient = (*UPnPClient)(nil)

// newUPnPClient creates and initializes a new UPnP client. Internal constructor.
func newUPnPClient(dialAddr string) (*UPnPClient, error) {
	client := &UPnPClient{
		MappedPorts: make(map[uint16]string),
	}

	// Get local IP address (use shared helper)
	localIP, err := getLocalIP(dialAddr)
	if err != nil {
		return nil, fmt.Errorf("UPnP: failed to get local IP: %w", err)
	}
	client.LocalIP = localIP

	// Discover IGD
	igd, gwType, err := discoverIGD() // Use internal discovery
	if err != nil {
		return nil, fmt.Errorf("UPnP: failed to discover IGD: %w", err)
	}
	client.gateway = igd
	client.GatewayType = gwType

	// Get external IP
	externalIP, err := client.fetchExternalIP() // Use internal fetch
	if err != nil {
		// Non-fatal? Some routers might allow mapping without reporting external IP
		log.Printf("UPnP: Warning - failed to get external IP: %v", err)
		client.ExternalIP = "" // Set to empty but continue
	} else {
		client.ExternalIP = externalIP
	}

	return client, nil
}

// discoverIGD tries to discover an Internet Gateway Device (internal helper)
func discoverIGD() (interface{}, string, error) {
	// Try InternetGatewayDevice v2 first
	// Increased timeout might help on slower networks/routers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ig2cps, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
	if err == nil && len(ig2cps) > 0 {
		return ig2cps[0], "IGDv2-IP2", nil
	}
	logFailure("IGDv2-IP2", err) // Log discovery failure

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ig2ppcps, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx)
	if err == nil && len(ig2ppcps) > 0 {
		return ig2ppcps[0], "IGDv2-PPP1", nil
	}
	logFailure("IGDv2-PPP1", err)

	// Fall back to InternetGatewayDevice v1
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ig1cps, _, err := internetgateway1.NewWANIPConnection1ClientsCtx(ctx)
	if err == nil && len(ig1cps) > 0 {
		return ig1cps[0], "IGDv1-IP1", nil
	}
	logFailure("IGDv1-IP1", err)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ig1ppcps, _, err := internetgateway1.NewWANPPPConnection1ClientsCtx(ctx)
	if err == nil && len(ig1ppcps) > 0 {
		return ig1ppcps[0], "IGDv1-PPP1", nil
	}
	logFailure("IGDv1-PPP1", err)

	return nil, "", fmt.Errorf("no compatible UPnP IGD found after checking all types")
}

// Helper to log discovery failures concisely
func logFailure(igdType string, err error) {
	if err != nil && err != context.DeadlineExceeded && !strings.Contains(err.Error(), "context deadline exceeded") {
		log.Printf("UPnP Discovery: %s probe failed: %v", igdType, err)
	} else if err != nil {
		log.Printf("UPnP Discovery: %s probe timed out or failed: %v", igdType, err)
	}
}

// fetchExternalIP retrieves the external IP address from the gateway (internal helper)
func (c *UPnPClient) fetchExternalIP() (string, error) {
	var ip string
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // Timeout for the SOAP call
	defer cancel()

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		ip, err = g.GetExternalIPAddressCtx(ctx)
	case *internetgateway2.WANPPPConnection1:
		ip, err = g.GetExternalIPAddressCtx(ctx)
	case *internetgateway1.WANIPConnection1:
		ip, err = g.GetExternalIPAddressCtx(ctx)
	case *internetgateway1.WANPPPConnection1:
		ip, err = g.GetExternalIPAddressCtx(ctx)
	default:
		return "", fmt.Errorf("UPnP: unknown gateway type (%T)", c.gateway)
	}

	if err != nil {
		return "", fmt.Errorf("UPnP: GetExternalIPAddress failed: %w", err)
	}
	return ip, nil
}

// GetExternalIP returns the cached external IP address.
func (c *UPnPClient) GetExternalIP() string {
	return c.ExternalIP
}

// GetLocalIP returns the cached local IP address.
func (c *UPnPClient) GetLocalIP() string {
	return c.LocalIP
}

// GetType returns the discovered gateway type.
func (c *UPnPClient) GetType() string {
	return c.GatewayType
}

// AddPortMapping creates a new port mapping.
func (c *UPnPClient) AddPortMapping(protocol string, externalPort, internalPort uint16, description string, leaseDuration uint32) error {
	protocolUpper := standardizeProtocol(protocol) // Use shared helper
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // Timeout for the SOAP call
	defer cancel()

	log.Printf("UPnP AddPortMapping CALL: Proto=%s, ExtPort=%d, IntPort=%d, IntClient=%s, Desc=%s, Lease=%d",
		protocolUpper, externalPort, internalPort, c.LocalIP, description, leaseDuration)

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		err = g.AddPortMappingCtx(ctx,
			"",            // Remote host (empty for wildcard)
			externalPort,  // External port
			protocolUpper, // Protocol (TCP/UDP)
			internalPort,  // Internal port
			c.LocalIP,     // Internal client
			true,          // Enabled
			description,   // Description
			leaseDuration, // Lease duration in seconds (0 for unlimited)
		)
	case *internetgateway2.WANPPPConnection1:
		err = g.AddPortMappingCtx(ctx,
			"", externalPort, protocolUpper, internalPort, c.LocalIP, true, description, leaseDuration,
		)
	case *internetgateway1.WANIPConnection1:
		err = g.AddPortMappingCtx(ctx,
			"", externalPort, protocolUpper, internalPort, c.LocalIP, true, description, leaseDuration,
		)
	case *internetgateway1.WANPPPConnection1:
		err = g.AddPortMappingCtx(ctx,
			"", externalPort, protocolUpper, internalPort, c.LocalIP, true, description, leaseDuration,
		)
	default:
		return fmt.Errorf("UPnP: unknown gateway type (%T) for AddPortMapping", c.gateway)
	}

	if err != nil {
		// Provide more context on common errors if possible
		if strings.Contains(err.Error(), "ConflictInMappingEntry") {
			return fmt.Errorf("UPnP: AddPortMapping failed - port %d/%s may already be mapped: %w", externalPort, protocolUpper, err)
		}
		if strings.Contains(err.Error(), "OnlyPermanentLeasesSupported") && leaseDuration != 0 {
			log.Printf("UPnP: Router only supports permanent leases, retrying with lease duration 0...")
			// Retry with lease 0 if the error indicates only permanent leases are supported
			return c.AddPortMapping(protocol, externalPort, internalPort, description, 0)
		}
		return fmt.Errorf("UPnP: AddPortMapping failed for %d/%s: %w", externalPort, protocolUpper, err)
	}

	// Store the mapping for later cleanup
	c.MappedPorts[externalPort] = protocolUpper
	return nil
}

// DeletePortMapping removes a port mapping.
func (c *UPnPClient) DeletePortMapping(protocol string, externalPort uint16) error {
	protocolUpper := standardizeProtocol(protocol) // Use shared helper
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // Timeout for the SOAP call
	defer cancel()

	log.Printf("UPnP DeletePortMapping CALL: Proto=%s, ExtPort=%d", protocolUpper, externalPort)

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		err = g.DeletePortMappingCtx(ctx, "", externalPort, protocolUpper)
	case *internetgateway2.WANPPPConnection1:
		err = g.DeletePortMappingCtx(ctx, "", externalPort, protocolUpper)
	case *internetgateway1.WANIPConnection1:
		err = g.DeletePortMappingCtx(ctx, "", externalPort, protocolUpper)
	case *internetgateway1.WANPPPConnection1:
		err = g.DeletePortMappingCtx(ctx, "", externalPort, protocolUpper)
	default:
		return fmt.Errorf("UPnP: unknown gateway type (%T) for DeletePortMapping", c.gateway)
	}

	if err != nil {
		// Don't remove from map if deletion failed, maybe retry later?
		// Log error but potentially allow cleanup to proceed with others
		if strings.Contains(err.Error(), "NoSuchEntryInArray") {
			log.Printf("UPnP: DeletePortMapping for %d/%s failed, likely already removed: %v", externalPort, protocolUpper, err)
			// Treat as success for cleanup purposes, remove from map
			delete(c.MappedPorts, externalPort)
			return nil
		}
		return fmt.Errorf("UPnP: DeletePortMapping failed for %d/%s: %w", externalPort, protocolUpper, err)
	}

	// Remove from tracked mappings on successful deletion request
	delete(c.MappedPorts, externalPort)
	return nil
}

// CleanupAllMappings removes all port mappings created by this client instance.
func (c *UPnPClient) CleanupAllMappings() {
	if c == nil || c.gateway == nil {
		return // Not initialized or already cleaned up
	}
	log.Printf("UPnP Cleanup: Cleaning up %d mapped ports for %s...", len(c.MappedPorts), c.GetType())
	// Create a copy of keys/values to avoid concurrent modification issues during iteration
	portsToClean := make(map[uint16]string)
	for port, proto := range c.MappedPorts {
		portsToClean[port] = proto
	}

	for port, protocol := range portsToClean {
		err := c.DeletePortMapping(protocol, port)
		if err != nil {
			// Log failure but continue trying to clean up others
			log.Printf("UPnP Cleanup: Failed to remove port mapping %d/%s: %v", port, protocol, err)
		} else {
			log.Printf("UPnP Cleanup: Removed port mapping %d/%s", port, protocol)
			// DeletePortMapping already removed it from the live map if successful
		}
	}
	log.Printf("UPnP Cleanup: Finished.")
	// Optionally nil out fields to aid GC?
	// c.gateway = nil
	// c.MappedPorts = nil
}

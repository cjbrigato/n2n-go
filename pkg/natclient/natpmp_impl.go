package natclient

import (
	"fmt"
	"n2n-go/pkg/log"
	"net"
	"strings"
	"time"

	"github.com/jackpal/gateway" // Import the correct gateway package
	natpmp "github.com/jackpal/go-nat-pmp"
)

// natPMPPortMappingInfo stores info needed for potential cleanup
type natPMPPortMappingInfo struct {
	Protocol     string
	InternalPort uint16
	ExternalPort uint16
	// Lifetime     int // We don't strictly need to store lifetime for deletion
}

// NATPMPClient encapsulates NAT-PMP/PCP functionality
type NATPMPClient struct {
	client      *natpmp.Client
	gatewayIP   net.IP // Store the gateway IP used
	LocalIP     string
	ExternalIP  string
	MappedPorts map[uint16]natPMPPortMappingInfo // Map external port to info
}

// Compile-time check to ensure NATPMPClient implements NATClient
var _ NATClient = (*NATPMPClient)(nil)

// newNATPMPClient creates and initializes a new NAT-PMP/PCP client. Internal constructor.
func newNATPMPClient(dialAddr string) (*NATPMPClient, error) {
	client := &NATPMPClient{
		MappedPorts: make(map[uint16]natPMPPortMappingInfo),
	}

	// Get local IP address (use shared helper)
	localIP, err := getLocalIP(dialAddr)
	if err != nil {
		return nil, fmt.Errorf("NAT-PMP/PCP: failed to get local IP: %w", err)
	}
	client.LocalIP = localIP

	// Find the gateway IP using the correct package
	gatewayIP, err := discoverGatewayIP() // Use internal helper calling gateway.DiscoverGateway()
	if err != nil {
		log.Printf("NAT-PMP/PCP: Warning - automatic gateway discovery failed: %v. Trying fallback guess.", err)
		// Fallback: Guess gateway is x.y.z.1 based on local IP x.y.z.w
		parsedLocalIP := net.ParseIP(localIP)
		if parsedLocalIP != nil && parsedLocalIP.To4() != nil {
			ipBytes := parsedLocalIP.To4()
			gatewayIP = net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], 1)
			log.Printf("NAT-PMP/PCP: Assuming gateway is %s based on local IP %s", gatewayIP.String(), localIP)
		} else {
			// If we can't even guess, return error
			return nil, fmt.Errorf("NAT-PMP/PCP: failed to determine gateway IP automatically or by guessing: %w", err)
		}
	}

	log.Printf("NAT-PMP/PCP: Using gateway IP: %s", gatewayIP.String())
	client.gatewayIP = gatewayIP

	// Create the NAT-PMP/PCP client instance targeting the discovered/guessed gateway
	// Provide a timeout for initial communication attempts by the client
	natPMPClient := natpmp.NewClientWithTimeout(client.gatewayIP, 3*time.Second) // 3 sec timeout for operations
	client.client = natPMPClient

	// Get external IP (best effort)
	response, err := client.client.GetExternalAddress()
	if err != nil {
		// Treat timeout as non-fatal warning, maybe mapping will still work
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			log.Printf("NAT-PMP/PCP: Warning - timed out getting external IP: %v", err)
			client.ExternalIP = "" // Mark as unknown
		} else {
			// Other errors might be more problematic but still try to continue
			log.Printf("NAT-PMP/PCP: Warning - error getting external IP: %v", err)
			client.ExternalIP = "" // Mark as unknown
		}
	} else {
		// Correctly convert [4]byte to net.IP then to string
		client.ExternalIP = net.IP(response.ExternalIPAddress[:]).String()
	}

	return client, nil
}

// discoverGatewayIP tries to find the gateway using the gateway package (internal helper)
func discoverGatewayIP() (net.IP, error) {
	// Use gateway.DiscoverGateway() - no timeout parameter here
	gw, err := gateway.DiscoverGateway()
	if err != nil {
		return nil, fmt.Errorf("gateway.DiscoverGateway failed: %w", err)
	}
	if gw == nil {
		// This case shouldn't happen if err is nil, but check defensively
		return nil, fmt.Errorf("gateway.DiscoverGateway returned nil IP without error")
	}
	return gw, nil
}

// GetExternalIP returns the cached external IP address.
func (c *NATPMPClient) GetExternalIP() string {
	return c.ExternalIP
}

// GetLocalIP returns the cached local IP address.
func (c *NATPMPClient) GetLocalIP() string {
	return c.LocalIP
}

// GetType returns the client type.
func (c *NATPMPClient) GetType() string {
	return "NAT-PMP/PCP"
}

// AddPortMapping creates a new port mapping using NAT-PMP/PCP.
func (c *NATPMPClient) AddPortMapping(protocol string, externalPort, internalPort uint16, description string, leaseDuration uint32) error {
	protoLower := strings.ToLower(protocol)
	if protoLower != "udp" && protoLower != "tcp" {
		return fmt.Errorf("NAT-PMP/PCP: invalid protocol: %s", protocol)
	}

	// go-nat-pmp uses int for lifetime.
	// Map 0 lease request to a default positive value, as 0 means delete.
	lifetimeSeconds := int(leaseDuration)
	if lifetimeSeconds <= 0 {
		lifetimeSeconds = 3600 // Default to 1 hour if 0 or negative requested
		log.Printf("NAT-PMP/PCP: Requested lease duration <= 0, using default %d seconds", lifetimeSeconds)
	}

	// Description is not used by the protocol itself, but good to log.
	log.Printf("NAT-PMP/PCP AddPortMapping CALL: Proto=%s, IntPort=%d, ExtPort=%d, Lifetime=%d desc=(%s)",
		protoLower, internalPort, externalPort, lifetimeSeconds, description)

	// The library handles PCP/PMP negotiation internally.
	// Result contains the *actual* assigned external port and lifetime.
	res, err := c.client.AddPortMapping(protoLower, int(internalPort), int(externalPort), lifetimeSeconds)
	if err != nil {
		return fmt.Errorf("NAT-PMP/PCP: AddPortMapping failed for int:%d/ext:%d: %w", internalPort, externalPort, err)
	}

	// Important: Use the actual external port returned by the router,
	// as it might differ from the requested one if there was a conflict.
	actualExternalPort := uint16(res.MappedExternalPort)
	actualLifetime := res.PortMappingLifetimeInSeconds // uint32

	if actualExternalPort != externalPort {
		log.Printf("NAT-PMP/PCP: Warning - router assigned external port %d instead of requested %d", actualExternalPort, externalPort)
		// Decide how to handle this: error out, or use the assigned port?
		// For now, let's use the assigned port but store mapping under it.
	}
	log.Printf("NAT-PMP/PCP: Router granted mapping for external port %d, lifetime %d seconds", actualExternalPort, actualLifetime)

	// Store mapping info using the *actual* external port assigned by the router
	c.MappedPorts[actualExternalPort] = natPMPPortMappingInfo{
		Protocol:     protoLower,
		InternalPort: internalPort, // Deletion needs internal port
		ExternalPort: actualExternalPort,
	}

	return nil
}

// DeletePortMapping removes a port mapping using NAT-PMP/PCP.
// Requires the external port that was *actually* mapped (could be different from requested).
func (c *NATPMPClient) DeletePortMapping(protocol string, externalPort uint16) error {
	mappingInfo, exists := c.MappedPorts[externalPort]
	if !exists {
		log.Printf("NAT-PMP/PCP: No mapping info found for external port %d/%s to delete (maybe already removed or assigned differently?).", externalPort, protocol)
		return nil // Treat as non-error if not tracked
	}

	// Standardize protocol just in case, though stored one should be correct
	protoLower := strings.ToLower(protocol)
	if protoLower != mappingInfo.Protocol {
		log.Printf("NAT-PMP/PCP: Protocol mismatch for external port %d delete request (expected %s, got %s). Using stored: %s",
			externalPort, mappingInfo.Protocol, protoLower, mappingInfo.Protocol)
		protoLower = mappingInfo.Protocol // Use the stored protocol
	}

	internalPort := mappingInfo.InternalPort // Deletion uses internal port

	log.Printf("NAT-PMP/PCP DeletePortMapping CALL: Proto=%s, IntPort=%d (for ExtPort=%d)",
		protoLower, internalPort, externalPort)

	// To delete, request a mapping for the *internal* port with lifetime 0.
	// We don't care about the external port requested here (can be 0 or the original).
	_, err := c.client.AddPortMapping(protoLower, int(internalPort), 0, 0) // Lifetime 0 deletes
	if err != nil {
		// Don't remove from map if deletion failed, caller might retry
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("NAT-PMP/PCP: DeletePortMapping timed out for int:%d/ext:%d: %w", internalPort, externalPort, err)
		}
		// Check for specific NAT-PMP/PCP errors if the library exposes them
		return fmt.Errorf("NAT-PMP/PCP: DeletePortMapping (requesting lifetime 0) failed for int:%d/ext:%d: %w", internalPort, externalPort, err)
	}

	// Remove from tracked mappings on successful deletion request
	delete(c.MappedPorts, externalPort)
	return nil
}

// CleanupAllMappings removes all port mappings created by this client instance.
func (c *NATPMPClient) CleanupAllMappings() {
	if c == nil || c.client == nil {
		return // Not initialized
	}
	log.Printf("NAT-PMP/PCP Cleanup: Cleaning up %d mapped ports for %s...", len(c.MappedPorts), c.GetType())
	// Create a copy to avoid concurrent modification issues
	portsToClean := make(map[uint16]natPMPPortMappingInfo)
	for port, info := range c.MappedPorts {
		portsToClean[port] = info
	}

	for extPort, mappingInfo := range portsToClean {
		err := c.DeletePortMapping(mappingInfo.Protocol, extPort) // Use the actual external port
		if err != nil {
			log.Printf("NAT-PMP/PCP Cleanup: Failed to remove port mapping (ext: %d, int: %d)/%s: %v",
				extPort, mappingInfo.InternalPort, mappingInfo.Protocol, err)
			// Continue with others
		} else {
			log.Printf("NAT-PMP/PCP Cleanup: Removed port mapping (ext: %d, int: %d)/%s",
				extPort, mappingInfo.InternalPort, mappingInfo.Protocol)
			// DeletePortMapping already removed it from the live map if successful
		}
	}
	log.Printf("NAT-PMP/PCP Cleanup: Finished.")
}

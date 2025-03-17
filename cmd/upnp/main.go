package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

type PortMapping struct {
	Protocol       string
	RemoteHost     string
	ExternalPort   string
	InternalPort   string
	InternalClient string
	Enabled        bool
	Description    string
	LeaseDuration  uint32
}

// UPnPClient encapsulates IGD (Internet Gateway Device) functionality
type UPnPClient struct {
	gateway     interface{}
	gatewayType string
	localIP     string
	externalIP  string
	mappedPorts map[uint16]string
}

// NewUPnPClient creates and initializes a new UPnP client
func NewUPnPClient() (*UPnPClient, error) {
	client := &UPnPClient{
		mappedPorts: make(map[uint16]string),
	}

	// Get local IP address
	localIP, err := getLocalIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get local IP: %w", err)
	}
	client.localIP = localIP

	// Discover IGD
	if err := client.discoverIGD(); err != nil {
		return nil, fmt.Errorf("failed to discover IGD: %w", err)
	}

	// Get external IP
	externalIP, err := client.GetExternalIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get external IP: %w", err)
	}
	client.externalIP = externalIP

	return client, nil
}

// discoverIGD tries to discover an Internet Gateway Device
func (c *UPnPClient) discoverIGD() error {

	// Try InternetGatewayDevice v2 first
	ig2cps, _, err := internetgateway2.NewWANIPConnection2Clients()
	if err == nil && len(ig2cps) > 0 {
		c.gateway = ig2cps[0]
		c.gatewayType = "IGDv2-IP2"
		return nil
	}

	ig2ppcps, _, err := internetgateway2.NewWANPPPConnection1Clients()
	if err == nil && len(ig2ppcps) > 0 {
		c.gateway = ig2ppcps[0]
		c.gatewayType = "IGDv2-PPP1"
		return nil
	}

	// Fall back to InternetGatewayDevice v1
	ig1cps, _, err := internetgateway1.NewWANIPConnection1Clients()
	if err == nil && len(ig1cps) > 0 {
		c.gateway = ig1cps[0]
		c.gatewayType = "IGDv1-IP1"
		return nil
	}

	ig1ppcps, _, err := internetgateway1.NewWANPPPConnection1Clients()
	if err == nil && len(ig1ppcps) > 0 {
		c.gateway = ig1ppcps[0]
		c.gatewayType = "IGDv1-PPP1"
		return nil
	}

	return fmt.Errorf("no compatible IGD found")
}

// GetExternalIP returns the external IP address of the gateway
func (c *UPnPClient) GetExternalIP() (string, error) {
	var ip string
	var err error

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		ip, err = g.GetExternalIPAddress()
	case *internetgateway2.WANPPPConnection1:
		ip, err = g.GetExternalIPAddress()
	case *internetgateway1.WANIPConnection1:
		ip, err = g.GetExternalIPAddress()
	case *internetgateway1.WANPPPConnection1:
		ip, err = g.GetExternalIPAddress()
	default:
		return "", fmt.Errorf("unknown gateway type")
	}

	if err != nil {
		return "", err
	}
	return ip, nil
}

// AddPortMapping creates a new port mapping
func (c *UPnPClient) AddPortMapping(protocol string, externalPort, internalPort uint16, description string, leaseDuration uint32) error {
	// Standardize protocol string
	protocolUpper := "TCP"
	if protocol == "udp" || protocol == "UDP" {
		protocolUpper = "UDP"
	}

	// Enable port mapping
	var err error

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		err = g.AddPortMapping(
			"",            // Remote host (empty for wildcard)
			externalPort,  // External port
			protocolUpper, // Protocol (TCP/UDP)
			internalPort,  // Internal port
			c.localIP,     // Internal client
			true,          // Description
			description,   // Enabled
			leaseDuration, // Lease duration in seconds (0 for unlimited)
		)
	case *internetgateway2.WANPPPConnection1:
		err = g.AddPortMapping(
			"",
			externalPort,
			protocolUpper,
			internalPort,
			c.localIP,
			true,
			description,
			leaseDuration,
		)
	case *internetgateway1.WANIPConnection1:
		err = g.AddPortMapping(
			"",
			externalPort,
			protocolUpper,
			internalPort,
			c.localIP,
			true,
			description,
			leaseDuration,
		)
	case *internetgateway1.WANPPPConnection1:
		err = g.AddPortMapping(
			"",
			externalPort,
			protocolUpper,
			internalPort,
			c.localIP,
			true,
			description,
			leaseDuration,
		)
	default:
		return fmt.Errorf("unknown gateway type")
	}

	if err != nil {
		return err
	}

	// Store the mapping for later cleanup
	c.mappedPorts[externalPort] = protocolUpper
	return nil
}

// DeletePortMapping removes a port mapping
func (c *UPnPClient) DeletePortMapping(protocol string, externalPort uint16) error {
	// Standardize protocol string
	protocolUpper := "TCP"
	if protocol == "udp" || protocol == "UDP" {
		protocolUpper = "UDP"
	}

	var err error

	switch g := c.gateway.(type) {
	case *internetgateway2.WANIPConnection2:
		err = g.DeletePortMapping("", externalPort, protocolUpper)
	case *internetgateway2.WANPPPConnection1:
		err = g.DeletePortMapping("", externalPort, protocolUpper)
	case *internetgateway1.WANIPConnection1:
		err = g.DeletePortMapping("", externalPort, protocolUpper)
	case *internetgateway1.WANPPPConnection1:
		err = g.DeletePortMapping("", externalPort, protocolUpper)
	default:
		return fmt.Errorf("unknown gateway type")
	}

	if err != nil {
		return err
	}

	// Remove from tracked mappings
	delete(c.mappedPorts, externalPort)
	return nil
}

// GetPortMappings attempts to retrieve existing port mappings
// Note: Not all routers support this functionality
func (c *UPnPClient) GetPortMappings() error {
	// This implementation is limited since many routers don't properly implement
	// GetGenericPortMappingEntry or don't allow listing all mappings

	// Try to get up to 100 mappings (arbitrary limit)
	for i := 0; i < 100; i++ {
		var portMapping *PortMapping
		var err error

		switch g := c.gateway.(type) {
		case *internetgateway2.WANIPConnection2:
			portMapping, err = getMapping(g, uint16(i))
		case *internetgateway1.WANIPConnection1:
			portMapping, err = getMapping(g, uint16(i))
		// Some gateway types don't support this operation
		default:
			return fmt.Errorf("gateway type doesn't support listing mappings")
		}

		if err != nil {
			// We've likely reached the end of mappings or encountered an error
			if i == 0 {
				return fmt.Errorf("failed to get any port mappings: %w", err)
			}
			break
		}

		if portMapping == nil {
			break
		}

		fmt.Printf("Mapping %d: %s port %s → %s:%s (%s) - Lease: %s\n",
			i,
			portMapping.Protocol,
			portMapping.ExternalPort,
			portMapping.InternalClient,
			portMapping.InternalPort,
			portMapping.Description,
			portMapping.LeaseDuration)
	}

	return nil
}

// Helper function to get a port mapping by index
func getMapping(g interface{}, index uint16) (*PortMapping, error) {
	var protocol, remoteHost, internalClient, description string
	var portMapEnabled bool
	var leaseDuration uint32
	var externalPort, internalPort uint16
	var err error

	switch gateway := g.(type) {
	case *internetgateway2.WANIPConnection2:
		remoteHost, externalPort, protocol, internalPort, internalClient,
			portMapEnabled, description, leaseDuration, err = gateway.GetGenericPortMappingEntry(index)
	case *internetgateway1.WANIPConnection1:
		remoteHost, externalPort, protocol, internalPort, internalClient,
			portMapEnabled, description, leaseDuration, err = gateway.GetGenericPortMappingEntry(index)
	default:
		return nil, fmt.Errorf("unsupported gateway type for GetGenericPortMappingEntry")
	}

	if err != nil {
		return nil, err
	}

	return &PortMapping{
		Protocol:       protocol,
		RemoteHost:     remoteHost,
		ExternalPort:   strconv.FormatUint(uint64(externalPort), 10),
		InternalPort:   strconv.FormatUint(uint64(internalPort), 10),
		InternalClient: internalClient,
		Enabled:        portMapEnabled,
		Description:    description,
		LeaseDuration:  leaseDuration,
	}, nil
}

// CleanupAllMappings removes all port mappings created by this client
func (c *UPnPClient) CleanupAllMappings() {
	for port, protocol := range c.mappedPorts {
		err := c.DeletePortMapping(protocol, port)
		if err != nil {
			log.Printf("Failed to remove port mapping %d/%s: %v", port, protocol, err)
		} else {
			log.Printf("Removed port mapping %d/%s", port, protocol)
		}
	}
}

// getLocalIP returns the non-loopback local IP of the host
func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addrs {
		// Check the address type and if it's not a loopback
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no suitable local IP found")
}

func main() {
	var (
		getIP         = flag.Bool("get-ip", false, "Get external IP address")
		listMappings  = flag.Bool("list", false, "List existing port mappings")
		protocol      = flag.String("protocol", "tcp", "Protocol (tcp/udp)")
		externalPort  = flag.Uint("external-port", 0, "External port")
		internalPort  = flag.Uint("internal-port", 0, "Internal port")
		description   = flag.String("desc", "Go UPnP Client", "Mapping description")
		leaseDuration = flag.Uint("lease", 0, "Lease duration in seconds (0 for unlimited)")
		remove        = flag.Bool("remove", false, "Remove a port mapping")
	)
	flag.Parse()

	// Create UPnP client
	client, err := NewUPnPClient()
	if err != nil {
		log.Fatalf("Failed to initialize UPnP client: %v", err)
	}

	log.Printf("Successfully connected to IGD (%s)", client.gatewayType)
	log.Printf("Local IP: %s", client.localIP)
	log.Printf("External IP: %s", client.externalIP)

	// Set up signal handling for cleanup
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		log.Println("Cleaning up port mappings before exit...")
		client.CleanupAllMappings()
		os.Exit(0)
	}()

	// Process commands based on flags
	switch {
	case *getIP:
		fmt.Println("External IP:", client.externalIP)

	case *listMappings:
		log.Println("Attempting to list port mappings (may not work on all routers)...")
		if err := client.GetPortMappings(); err != nil {
			log.Printf("Error listing port mappings: %v", err)
		}

	case *remove:
		if *externalPort == 0 {
			log.Fatal("External port is required for removal")
		}
		log.Printf("Removing port mapping for %s port %d...", *protocol, *externalPort)
		if err := client.DeletePortMapping(*protocol, uint16(*externalPort)); err != nil {
			log.Fatalf("Failed to remove port mapping: %v", err)
		}
		log.Println("Port mapping removed successfully")

	case *externalPort > 0 && *internalPort > 0:
		// Add a new port mapping
		log.Printf("Creating port mapping: %s %d -> %s:%d (%s)",
			*protocol, *externalPort, client.localIP, *internalPort, *description)

		err := client.AddPortMapping(
			*protocol,
			uint16(*externalPort),
			uint16(*internalPort),
			*description,
			uint32(*leaseDuration),
		)
		if err != nil {
			log.Fatalf("Failed to add port mapping: %v", err)
		}
		log.Println("Port mapping added successfully")
		log.Println("Press Ctrl+C to remove mapping and exit")

		// Keep the program running
		select {}

	default:
		log.Println("No action specified. Use -help for usage information.")
	}
}

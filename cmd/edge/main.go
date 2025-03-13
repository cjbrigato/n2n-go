package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"n2n-go/pkg/edge"
)

// Version information will be set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Config holds the edge node configuration
type Config struct {
	EdgeID            string
	Community         string
	TapName           string
	LocalPort         int
	SupernodeAddr     string
	HeartbeatInterval time.Duration
}

// configureInterface brings up the TAP interface and assigns the given IP address.
func configureInterface(ifName, ipAddr string) error {
	log.Printf("Configuring interface %s with IP %s", ifName, ipAddr)

	// Bring the interface up
	cmdUp := exec.Command("ip", "link", "set", "dev", ifName, "up")
	if err := cmdUp.Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Assign IP address
	cmdAddr := exec.Command("ip", "addr", "add", ipAddr, "dev", ifName)
	if err := cmdAddr.Run(); err != nil {
		return fmt.Errorf("failed to assign IP address: %w", err)
	}

	return nil
}

func main() {
	log.Printf("n2n-go edge node %s (built %s)", Version, BuildTime)

	// Parse command-line flags
	cfg := Config{}
	flag.StringVar(&cfg.EdgeID, "id", "", "Unique edge identifier (e.g., hostname)")
	flag.StringVar(&cfg.Community, "community", "default", "Community name")
	flag.StringVar(&cfg.TapName, "tap", "n2n_tap0", "TAP interface name")
	flag.IntVar(&cfg.LocalPort, "port", 0, "Local UDP port (0 for system-assigned)")
	flag.StringVar(&cfg.SupernodeAddr, "supernode", "", "Supernode address (host:port)")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat", 30*time.Second, "Heartbeat interval")
	flag.Parse()

	// Validate configuration
	if cfg.EdgeID == "" || cfg.SupernodeAddr == "" {
		log.Println("Edge ID and supernode address are required.")
		flag.Usage()
		os.Exit(1)
	}

	// Create edge client
	client, err := edge.NewEdgeClient(
		cfg.EdgeID,
		cfg.Community,
		cfg.TapName,
		cfg.LocalPort,
		cfg.SupernodeAddr,
		cfg.HeartbeatInterval,
	)
	if err != nil {
		log.Fatalf("Failed to create edge client: %v", err)
	}

	// Setup clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Handle signals in a separate goroutine
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, shutting down gracefully...", sig)
		client.Close()
		os.Exit(0)
	}()

	// Register with supernode
	log.Printf("Registering with supernode at %s...", cfg.SupernodeAddr)
	if err := client.Register(); err != nil {
		log.Fatalf("Edge registration failed: %v", err)
	}

	// Validate virtual IP assignment
	if client.VirtualIP == "" {
		log.Fatalf("No virtual IP assigned by supernode")
	}

	// Configure network interface
	if err := configureInterface(cfg.TapName, client.VirtualIP); err != nil {
		log.Fatalf("Failed to configure TAP interface: %v", err)
	}
	log.Printf("TAP interface %s configured with virtual IP %s", cfg.TapName, client.VirtualIP)

	// Send gratuitous ARP to announce our presence on the virtual network
	if err := edge.SendGratuitousARP(cfg.TapName, client.TAP.HardwareAddr(), net.ParseIP(client.VirtualIP)); err != nil {
		log.Printf("Warning: Failed to send gratuitous ARP: %v", err)
	} else {
		log.Printf("Gratuitous ARP sent successfully")
	}

	// Log connection details
	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("Edge %s registered on local UDP port %s. TAP interface: %s",
		cfg.EdgeID, strconv.Itoa(udpPort), cfg.TapName)

	// Start the client and block until closed
	log.Printf("Edge node is running. Press Ctrl+C to stop.")
	client.Run()

	// If we reach here, the client was closed from elsewhere
	log.Printf("Edge node has been shut down.")
}

package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"n2n-go/internal/fn"
	"n2n-go/pkg/edge"
	"n2n-go/pkg/protocol"
)

// Version information will be set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	log.Printf("n2n-go edge node %s (built %s)", Version, BuildTime)

	// Parse command-line flags
	cfg := edge.Config{}
	flag.StringVar(&cfg.EdgeID, "id", "", "Unique edge identifier (defaults to hostname if omitted)")
	flag.StringVar(&cfg.Community, "community", "default", "Community name")
	flag.StringVar(&cfg.TapName, "tap", "n2n_tap0", "TAP interface name")
	flag.IntVar(&cfg.LocalPort, "port", 0, "Local UDP port (0 for system-assigned)")
	flag.StringVar(&cfg.SupernodeAddr, "supernode", "", "Supernode address (host:port)")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat", 30*time.Second, "Heartbeat interval")
	flag.BoolVar(&cfg.UseCompactHeader, "compact", true, "Use compact header format (reduces overhead)")

	// Advanced options
	legacyMode := flag.Bool("legacy", false, "Force legacy header format (73-byte headers)")
	headerVersion := flag.Int("headerversion", 0, "Protocol header version (0=auto, 3=legacy, 4=compact)")

	flag.Parse()

	// Validate configuration
	if cfg.SupernodeAddr == "" {
		log.Println("Supernode address is required.")
		flag.Usage()
		os.Exit(1)
	}

	// Apply header format options
	if *legacyMode {
		cfg.UseCompactHeader = false
		cfg.ProtocolVersion = protocol.VersionLegacy
	} else if *headerVersion > 0 {
		cfg.ProtocolVersion = uint8(*headerVersion)

		// Ensure UseCompactHeader matches the specified version
		if cfg.ProtocolVersion == protocol.VersionCompact {
			cfg.UseCompactHeader = true
		} else if cfg.ProtocolVersion == protocol.VersionLegacy {
			cfg.UseCompactHeader = false
		}
	} else if cfg.UseCompactHeader {
		cfg.ProtocolVersion = protocol.VersionCompact
	} else {
		cfg.ProtocolVersion = protocol.VersionLegacy
	}

	log.Printf("Using %s header format (protocol version %d)",
		fn.T(cfg.UseCompactHeader, "compact", "legacy"), cfg.ProtocolVersion)

	// Create edge client
	client, err := edge.NewEdgeClient(cfg)
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

	if err := client.Setup(); err != nil {
		log.Fatalf("Edge setup failed: %v", err)
	}
	log.Printf("Edge setup successful")

	// Log connection details
	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("Edge %s registered on local UDP port %s. TAP interface: %s",
		cfg.EdgeID, strconv.Itoa(udpPort), cfg.TapName)

	// Log protocol version
	headerFormat := "legacy (73 bytes)"
	if client.UseCompactHeader() {
		headerFormat = "compact (28 bytes)"
	}
	log.Printf("Using %s header format - protocol v%d", headerFormat, client.ProtocolVersion())

	// Start the client and block until closed
	log.Printf("Edge node is running. Press Ctrl+C to stop.")
	client.Run()

	// If we reach here, the client was closed from elsewhere
	log.Printf("Edge node has been shut down.")
}

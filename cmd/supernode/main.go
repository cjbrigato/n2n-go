package main

import (
	"flag"
	"log"
	"n2n-go/pkg/supernode"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Version information will be set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Config holds the supernode configuration
type Config struct {
	Port            string
	CleanupInterval time.Duration
	ExpiryDuration  time.Duration
	DebugMode       bool
	CommunitySubnet string
	SubnetCIDR      int
}

func main() {

	go func() {
		http.ListenAndServe("0.0.0.0:3334", nil)
	}()
	
	log.Printf("n2n-go supernode %s (built %s)", Version, BuildTime)

	// Parse command-line flags
	cfg := Config{}
	flag.StringVar(&cfg.Port, "port", "7777", "UDP port for supernode")
	flag.DurationVar(&cfg.CleanupInterval, "cleanup", 5*time.Minute, "Cleanup interval for stale edges")
	flag.DurationVar(&cfg.ExpiryDuration, "expiry", 10*time.Minute, "Edge expiry duration")
	flag.BoolVar(&cfg.DebugMode, "debug", false, "Enable debug logging")
	flag.StringVar(&cfg.CommunitySubnet, "subnet", "10.128.0.0", "Base subnet for communities")
	flag.IntVar(&cfg.SubnetCIDR, "subnetcidr", 24, "CIDR prefix length for community subnets")
	flag.Parse()

	// Configure supernode
	addrStr := ":" + cfg.Port
	udpAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		log.Fatalf("Supernode: Failed to resolve UDP address %s: %v", addrStr, err)
	}

	// Start listening
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Supernode: Failed to listen on UDP %s: %v", addrStr, err)
	}
	log.Printf("Supernode: Listening on %s", addrStr)

	// Create supernode config
	snConfig := &supernode.Config{
		Debug:               cfg.DebugMode,
		CommunitySubnet:     cfg.CommunitySubnet,
		CommunitySubnetCIDR: cfg.SubnetCIDR,
		ExpiryDuration:      cfg.ExpiryDuration,
		CleanupInterval:     cfg.CleanupInterval,
		StrictHashChecking:  true,
	}

	// Create and start supernode
	sn := supernode.NewSupernodeWithConfig(conn, snConfig)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Handle signals in a separate goroutine
	go func() {
		sig := <-sigChan
		log.Printf("Supernode: Received signal %s, shutting down gracefully...", sig)
		sn.Shutdown()
		conn.Close()
		os.Exit(0)
	}()

	// Log startup information
	log.Printf("Supernode is running with:")
	log.Printf("- Cleanup interval: %v", cfg.CleanupInterval)
	log.Printf("- Edge expiry: %v", cfg.ExpiryDuration)
	log.Printf("- Base subnet: %s/%d", cfg.CommunitySubnet, cfg.SubnetCIDR)
	log.Printf("- Debug mode: %v", cfg.DebugMode)
	log.Printf("Enforced:")
	log.Printf("- Strict hash checking: %v", snConfig.StrictHashChecking)
	log.Printf("Press Ctrl+C to stop.")

	// Start processing packets
	sn.Listen()

	// If we reach here, the supernode has been stopped
	log.Printf("Supernode has been shut down.")
}

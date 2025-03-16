package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"n2n-go/pkg/supernode"
	"net"

	//_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const banner = "ICAgICBfXyAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICBfICAgICAKICAgIC8gL19fXyAgXyBfIF9fICBfX18gXyBfIF8gXyAgX19fICBfX3wgfF9fXyAKIF8gLyAoXy08IHx8IHwgJ18gXC8gLV8pICdffCAnIFwvIF8gXC8gX2AgLyAtXykKKF8pXy8vX18vXF8sX3wgLl9fL1xfX198X3wgfF98fF9cX19fL1xfXyxfXF9fX3wKLS0tLS0tLS0tLS0tLXxffC0tLS0tLS0tLS1AbjJuLWdvLSVzIChidWlsdCAlcykgICAKICA="

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
	EnableVFuze     bool
}

func main() {

	/*go func() {
		http.ListenAndServe("0.0.0.0:3334", nil)
	}()
	*/

	b, _ := base64.StdEncoding.DecodeString(banner)
	fmt.Printf(string(b), Version, BuildTime)

	// Parse command-line flags
	cfg := Config{}
	flag.StringVar(&cfg.Port, "port", "7777", "UDP port for supernode")
	flag.DurationVar(&cfg.CleanupInterval, "cleanup", 5*time.Minute, "Cleanup interval for stale edges")
	flag.DurationVar(&cfg.ExpiryDuration, "expiry", 10*time.Minute, "Edge expiry duration")
	flag.BoolVar(&cfg.DebugMode, "debug", false, "Enable debug logging")
	flag.BoolVar(&cfg.EnableVFuze, "enableFuze", true, "enable fuze fastpath")
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
		EnableVFuze:         true,
		UDPBufferSize:       2048 * 2048,
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
	log.Printf("- VFuze Data FastPath: %v", cfg.EnableVFuze)
	log.Printf("Enforced:")
	log.Printf("- Strict hash checking: %v", snConfig.StrictHashChecking)
	log.Printf("Press Ctrl+C to stop.")

	// Start processing packets
	sn.Listen()

	// If we reach here, the supernode has been stopped
	log.Printf("Supernode has been shut down.")
}

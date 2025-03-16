package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	//_ "net/http/pprof"

	"n2n-go/pkg/edge"
	"n2n-go/pkg/protocol"
)

const banner = "ICAgICBfICAgICAgIF8KICAgIC8gL19fIF9ffCB8X18gXyAgX18gICAKIF8gLyAvIC1fKSBfYCAvIF9gIC8gLV8pICAKKF8pXy9cX19fXF9fLF9cX18sIFxfX198Ci0tLS0tLS0tLS0tLS0tfF9fXy9AbjJuLWdvLSVzIChidWlsdCAlcykgICAgICAK"

// Version information will be set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {

	b, _ := base64.StdEncoding.DecodeString(banner)
	fmt.Printf(string(b), Version, BuildTime)

	// Parse command-line flags
	cfg := edge.Config{}
	flag.StringVar(&cfg.EdgeID, "id", "", "Unique edge identifier (defaults to hostname if omitted)")
	flag.StringVar(&cfg.Community, "community", "default", "Community name")
	flag.StringVar(&cfg.TapName, "tap", "n2n_tap0", "TAP interface name")
	flag.IntVar(&cfg.LocalPort, "port", 0, "Local UDP port (0 for system-assigned)")
	flag.BoolVar(&cfg.EnableVFuze, "enableFuze", true, "enable fuze fastpath")
	flag.StringVar(&cfg.SupernodeAddr, "supernode", "", "Supernode address (host:port)")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat", 30*time.Second, "Heartbeat interval")
	flag.IntVar(&cfg.UDPBufferSize, "udpbuffersize", 8192*8192, "UDP BUffer Sizes")

	flag.Parse()

	// Validate configuration
	if cfg.SupernodeAddr == "" {
		log.Println("Supernode address is required.")
		flag.Usage()
		os.Exit(1)
	}

	cfg.ProtocolVersion = protocol.VersionV

	log.Printf("Using %s header format (protocol version %d)",
		"protoV", cfg.ProtocolVersion)

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

	headerFormat := "protoV"
	log.Printf("Using %s header format - protocol v%d", headerFormat, client.ProtocolVersion())

	// Start the client and block until closed
	log.Printf("Edge node is running. Press Ctrl+C to stop.")
	client.Run()

	// If we reach here, the client was closed from elsewhere
	log.Printf("Edge node has been shut down.")
}

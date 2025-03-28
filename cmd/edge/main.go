// cmd/edge/main.go (Modified)
package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"n2n-go/pkg/edge"
)

const banner = "ICAgICBfICAgICAgIF8KICAgIC8gL19fIF9ffCB8X18gXyAgX18gICAKIF8gLyAvIC1fKSBfYCAvIF9gIC8gLV8pICAKKF8pXy9cX19fXF9fLF9cX18sIFxfX198Ci0tLS0tLS0tLS0tLS0tfF9fXy9AbjJuLWdvLSVzIChidWlsdCAlcykgICAgICAK"

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	b, _ := base64.StdEncoding.DecodeString(banner)
	fmt.Printf(string(b), Version, BuildTime)

	cfg, err := edge.LoadConfig() // Load config using Viper
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("Edge: using config file %s", cfg.ConfigFile)

	client, err := edge.NewEdgeClient(*cfg) // Pass the config struct
	if err != nil {
		log.Fatalf("Failed to create edge client: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, shutting down gracefully...", sig)
		client.Close()
		os.Exit(0)
	}()

	if err := client.InitialSetup(); err != nil {
		log.Printf("Edge setup failed: %v", err)
		client.Close()
		os.Exit(127)
	}
	log.Printf("Edge setup successful")
	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("Edge %s registered on local UDP port %d. TAP interface: %s",
		cfg.EdgeID, udpPort, cfg.TapName)
	headerFormat := "protoV"
	log.Printf("Using %s header format - protocol v%d", headerFormat, client.ProtocolVersion())
	log.Printf("Edge node is running. Press Ctrl+C to stop.")

	client.Run() // Start the edge client.

	log.Printf("Edge node has been shut down.")
}

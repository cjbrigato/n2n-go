// cmd/edge/main.go (Modified)
package main

import (
	"encoding/base64"
	"fmt"
	"n2n-go/pkg/log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"n2n-go/pkg/edge"

	_ "github.com/cjbrigato/ensure/linux" // edge only works on linux right now
)

const banner = "ICAgICBfICAgICAgIF8KICAgIC8gL19fIF9ffCB8X18gXyAgX18gICAKIF8gLyAvIC1fKSBfYCAvIF9gIC8gLV8pICAKKF8pXy9cX19fXF9fLF9cX18sIFxfX198Ci0tLS0tLS0tLS0tLS0tfF9fXy9AbjJuLWdvLSVzIChidWlsdCAlcykgICAgICAK"

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {

	err := log.Init("edge.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	log.Printf("starting edge...")

	b, _ := base64.StdEncoding.DecodeString(banner)
	fmt.Printf(string(b), Version, BuildTime)

	cfg, err := edge.LoadConfig() // Load config using Viper
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("using config file %s", cfg.ConfigFile)

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
		log.Printf("edge setup failed: %v", err)
		client.Close()
		os.Exit(127)
	}
	log.Printf("edge setup successful")
	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("edge %s registered on local UDP port %d. TAP interface: %s",
		cfg.EdgeID, udpPort, cfg.TapName)
	headerFormat := "protoV"
	log.Printf("Using %s header format - protocol v%d", headerFormat, client.ProtocolVersion())
	log.Printf("edge node is running. Press Ctrl+C to stop.")

	client.Run() // Start the edge client.

	log.Printf("edge node has been shut down.")
}

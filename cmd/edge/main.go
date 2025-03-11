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

	"n2n-go/pkg/edge"
)

func main() {
	// Command-line flags.
	edgeID := flag.String("id", "", "Unique edge identifier (e.g., MAC address)")
	community := flag.String("community", "default", "Community name")
	tapName := flag.String("tap", "n2n_tap0", "TAP interface name")
	localPort := flag.Int("port", 0, "Local UDP port (0 for system-assigned)")
	supernodeAddr := flag.String("supernode", "", "Supernode address (host:port)")
	heartbeatInterval := flag.Duration("heartbeat", 30*time.Second, "Heartbeat interval")
	flag.Parse()

	if *edgeID == "" || *supernodeAddr == "" {
		log.Println("Edge ID and supernode address are required.")
		flag.Usage()
		os.Exit(1)
	}

	// Create the edge client with a TAP interface.
	client, err := edge.NewEdgeClient(*edgeID, *community, *tapName, *localPort, *supernodeAddr, *heartbeatInterval)
	if err != nil {
		log.Fatalf("Failed to create edge client: %v", err)
	}
	defer client.Close()

	// Register with the supernode.
	if err := client.Register(); err != nil {
		log.Fatalf("Edge registration failed: %v", err)
	}

	// Setup OS signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, shutting down.", sig)
		client.Close()
		os.Exit(0)
	}()

	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("Edge %s registered successfully on local port %s. TAP interface: %s",
		*edgeID, strconv.Itoa(udpPort), *tapName)

	// Start processing traffic.
	client.Run()
}

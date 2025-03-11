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
	tunName := flag.String("tun", "n2n0", "TUN interface name")
	localPort := flag.Int("port", 0, "Local UDP port (0 for system-assigned)")
	supernodeAddr := flag.String("supernode", "", "Supernode address (host:port)")
	heartbeatInterval := flag.Duration("heartbeat", 30*time.Second, "Heartbeat interval")
	flag.Parse()

	if *edgeID == "" || *supernodeAddr == "" {
		log.Println("Edge ID and supernode address are required.")
		flag.Usage()
		os.Exit(1)
	}

	// Create the edge client.
	client, err := edge.NewEdgeClient(*edgeID, *community, *tunName, *localPort, *supernodeAddr, *heartbeatInterval)
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

	log.Printf("Edge %s registered successfully on local port %s. Entering main loop.",
		*edgeID, strconv.Itoa(client.Conn.LocalAddr().(*net.UDPAddr).Port))

	// Start processing traffic.
	client.Run()
}

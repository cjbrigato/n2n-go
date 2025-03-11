package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"n2n-go/pkg/supernode"
)

func main() {
	// Parse command-line flags.
	port := flag.String("port", "7777", "UDP port for supernode to listen on")
	cleanupInterval := flag.Duration("cleanup", 5*time.Minute, "Interval to run cleanup of stale edge registrations")
	expiryDuration := flag.Duration("expiry", 10*time.Minute, "Edge expiry duration (no heartbeat received)")
	flag.Parse()

	// Resolve and listen on the specified UDP port.
	addrStr := ":" + *port
	udpAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		log.Fatalf("Failed to resolve UDP address %s: %v", addrStr, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Failed to listen on UDP %s: %v", addrStr, err)
	}
	defer conn.Close()
	log.Printf("Supernode is running on %s", addrStr)

	// Create a new Supernode instance.
	sn := supernode.NewSupernode(conn)

	// Start a goroutine to cleanup stale edges periodically.
	go func() {
		ticker := time.NewTicker(*cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			sn.CleanupStaleEdges(*expiryDuration)
		}
	}()

	// Setup OS signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, shutting down.", sig)
		conn.Close()
		os.Exit(0)
	}()

	// Start listening for incoming packets.
	sn.Listen()
}

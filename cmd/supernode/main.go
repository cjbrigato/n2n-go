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

	"n2n-go/pkg/supernode"
)

func main() {
	// Command-line flags.
	port := flag.Int("port", 7777, "UDP port for the supernode to listen on")
	staleDuration := flag.Duration("stale", 60*time.Second, "Duration after which an edge is considered stale and cleaned up")
	flag.Parse()

	// Resolve the UDP address.
	addr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(*port))
	if err != nil {
		log.Fatalf("Supernode: Failed to resolve UDP address: %v", err)
	}

	// Open the UDP connection.
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		log.Fatalf("Supernode: Failed to listen on UDP: %v", err)
	}

	// Create the Supernode instance with the configured stale edge expiry.
	sn := supernode.NewSupernode(conn, *staleDuration)

	// Setup signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Supernode: Received signal %s, shutting down.", sig)
		conn.Close()
		os.Exit(0)
	}()

	log.Printf("Supernode: Listening on %s", addr.String())
	sn.Listen()
}

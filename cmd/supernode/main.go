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
	port := flag.String("port", "7777", "UDP port for supernode")
	cleanupInterval := flag.Duration("cleanup", 5*time.Minute, "Cleanup interval for stale edges")
	expiryDuration := flag.Duration("expiry", 10*time.Minute, "Edge expiry duration")
	flag.Parse()

	addrStr := ":" + *port
	udpAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		log.Fatalf("Supernode: Failed to resolve UDP address %s: %v", addrStr, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Supernode: Failed to listen on UDP %s: %v", addrStr, err)
	}
	defer conn.Close()
	log.Printf("Supernode: Listening on %s", addrStr)

	sn := supernode.NewSupernode(conn, *expiryDuration, *cleanupInterval)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Supernode: Received signal %s, shutting down.", sig)
		conn.Close()
		os.Exit(0)
	}()

	sn.Listen()
}

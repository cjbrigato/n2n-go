package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"n2n-go/pkg/edge"
)

// configureInterface brings the TAP interface up and assigns the given IP address.
// This uses the `ip` command; ensure you have proper privileges.
func configureInterface(ifName, ipAddr string) error {
	// Bring the interface up.
	cmdUp := exec.Command("ip", "link", "set", "dev", ifName, "up")
	if err := cmdUp.Run(); err != nil {
		return err
	}
	// Assign IP address (assuming /24).
	ipWithMask := ipAddr + "/24"
	cmdAddr := exec.Command("ip", "addr", "add", ipWithMask, "dev", ifName)
	if err := cmdAddr.Run(); err != nil {
		return err
	}
	return nil
}

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

	// Use the virtual IP received from the supernode to configure the TAP interface.
	if client.VirtualIP == nil {
		log.Fatalf("No virtual IP assigned by supernode")
	}
	if err := configureInterface(*tapName, client.VirtualIP.String()); err != nil {
		log.Fatalf("Failed to configure TAP interface: %v", err)
	}
	log.Printf("TAP interface %s configured with virtual IP %s", *tapName, client.VirtualIP.String())

	// Setup OS signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, unregistering and shutting down.", sig)
		client.Close()
		os.Exit(0)
	}()

	udpPort := client.Conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("Edge %s registered successfully on local UDP port %s. TAP interface: %s",
		*edgeID, strconv.Itoa(udpPort), *tapName)

	// Start processing traffic.
	client.Run()
}

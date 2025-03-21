// cmd/supernode/main.go (Modified)
package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"n2n-go/pkg/supernode"
)

const banner = "ICAgICBfXyAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICBfICAgICAKICAgIC8gL19fXyAgXyBfIF9fICBfX18gXyBfIF8gXyAgX19fICBfX3wgfF9fXyAKIF8gLyAoXy08IHx8IHwgJ18gXC8gLV8pICdffCAnIFwvIF8gXC8gX2AgLyAtXykKKF8pXy8vX18vXF8sX3wgLl9fL1xfX198X3wgfF98fF9cX19fL1xfXyxfXF9fX3wKLS0tLS0tLS0tLS0tLXxffC0tLS0tLS0tLS1AbjJuLWdvLSVzIChidWlsdCAlcykK"

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	b, _ := base64.StdEncoding.DecodeString(banner)
	fmt.Printf(string(b), Version, BuildTime)

	cfg, err := supernode.LoadConfig() // Load config using Viper
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("Supernode: config file %s", cfg.ConfigFile)

	udpAddr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr) // Use the listen address
	if err != nil {
		log.Fatalf("Supernode: Failed to resolve UDP address %s: %v", cfg.ListenAddr, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Supernode: Failed to listen on UDP %s: %v", cfg.ListenAddr, err)
	}
	log.Printf("Supernode: Listening on %s", cfg.ListenAddr)

	sn := supernode.NewSupernodeWithConfig(conn, cfg) // Pass the config struct

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Supernode: Received signal %s, shutting down gracefully...", sig)
		sn.Shutdown()
		conn.Close()
		os.Exit(0)
	}()

	log.Printf("Supernode is running with:")
	log.Printf("- Cleanup interval: %v", cfg.CleanupInterval)
	log.Printf("- Edge expiry: %v", cfg.ExpiryDuration)
	log.Printf("- Base subnet: %s/%d", cfg.CommunitySubnet, cfg.CommunitySubnetCIDR)
	log.Printf("- Debug mode: %v", cfg.Debug)
	log.Printf("- VFuze Data FastPath: %v", cfg.EnableVFuze)
	log.Printf("- Listen Address: %v", cfg.ListenAddr)
	log.Printf("Enforced:")
	log.Printf("- Strict hash checking: %v", cfg.StrictHashChecking)

	log.Printf("Press Ctrl+C to stop.")
	sn.Listen()

	log.Printf("Supernode has been shut down.")
}

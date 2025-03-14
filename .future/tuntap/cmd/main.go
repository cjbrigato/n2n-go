package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/tuntap" // Replace with your actual package path
)

// Simple encryption example using AES-GCM
type AESEncryption struct {
	gcm       cipher.AEAD
	nonceSize int
}

func NewAESEncryption(keyHex string) (*AESEncryption, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &AESEncryption{
		gcm:       gcm,
		nonceSize: gcm.NonceSize(),
	}, nil
}

func (a *AESEncryption) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, a.nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Encrypt and append nonce
	ciphertext := a.gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (a *AESEncryption) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < a.nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and decrypt
	nonce := ciphertext[:a.nonceSize]
	ciphertext = ciphertext[a.nonceSize:]

	plaintext, err := a.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// Example packet processor for logs
type LoggingProcessor struct{}

func (l *LoggingProcessor) ProcessPacket(packet []byte, packetType tuntap.PacketType) ([]byte, error) {
	header, _, _ := tuntap.ParseEthernetHeader(packet)

	fmt.Printf("Packet: %s -> %s, Type: %v, Size: %d bytes\n",
		fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", header.Source[0], header.Source[1], header.Source[2], header.Source[3], header.Source[4], header.Source[5]),
		fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", header.Destination[0], header.Destination[1], header.Destination[2], header.Destination[3], header.Destination[4], header.Destination[5]),
		packetType,
		len(packet))

	return packet, nil
}

func main() {
	// Parse command line flags
	devName := flag.String("name", "tap0", "TAP device name")
	keyHex := flag.String("key", "6368616e676520746869732070617373776f726420746f206120736563726574", "AES encryption key (hex)")
	flag.Parse()

	// Create the configuration
	config := tuntap.Config{
		Name:        *devName,
		DevType:     tuntap.TAP,
		Persist:     false,
		Owner:       os.Getuid(),
		Group:       os.Getgid(),
		Permissions: 0600,
	}

	// Create the VPN TAP device
	vpnDev, err := tuntap.NewVPNDevice(config)
	if err != nil {
		log.Fatalf("Failed to create VPN device: %v", err)
	}
	defer vpnDev.Close()

	fmt.Printf("Created TAP device for VPN: %s\n", vpnDev.Name)

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Set up the encryption
	encryption, err := NewAESEncryption(*keyHex)
	if err != nil {
		log.Fatalf("Failed to initialize encryption: %v", err)
	}

	// Create and register processors
	vpnDev.RegisterProcessor(&LoggingProcessor{})

	// Create and register encryption processor
	encryptProcessor := tuntap.NewEncryptionProcessor(
		encryption.Encrypt,
		encryption.Decrypt,
	)
	vpnDev.RegisterProcessor(encryptProcessor)

	// Create and register firewall processor
	firewall := tuntap.NewFirewallProcessor()
	// Allow traffic to common private networks
	firewall.AddAllowedNetwork("10.0.0.0/8")
	firewall.AddAllowedNetwork("172.16.0.0/12")
	firewall.AddAllowedNetwork("192.168.0.0/16")
	vpnDev.RegisterProcessor(firewall)

	// Start packet processing
	vpnDev.StartPacketProcessing()

	// Print stats periodically
	statsTicker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-statsTicker.C:
				fmt.Printf("Stats: RX: %d packets (%d bytes), TX: %d packets (%d bytes), Errors: %d\n",
					vpnDev.Stats.PacketsReceived,
					vpnDev.Stats.BytesReceived,
					vpnDev.Stats.PacketsSent,
					vpnDev.Stats.BytesSent,
					vpnDev.Stats.Errors)
			case <-sigCh:
				return
			}
		}
	}()

	fmt.Println("VPN device is running. Press Ctrl+C to exit.")
	<-sigCh
	fmt.Println("Shutting down...")

	// Clean up
	statsTicker.Stop()
	vpnDev.StopPacketProcessing()
}

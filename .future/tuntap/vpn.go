// Package tuntap enhancements for VPN functionality
package tuntap

import (
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// EthernetHeader represents an Ethernet frame header
type EthernetHeader struct {
	Destination [6]byte
	Source      [6]byte
	EtherType   uint16
}

// Constants for Ethernet frame types
const (
	EtherTypeIPv4 = 0x0800
	EtherTypeARP  = 0x0806
	EtherTypeIPv6 = 0x86DD
)

// PacketType represents different packet protocols
type PacketType int

const (
	PacketTypeUnknown PacketType = iota
	PacketTypeIPv4
	PacketTypeIPv6
	PacketTypeARP
)

// PacketProcessor defines an interface for processing packets
type PacketProcessor interface {
	ProcessPacket(packet []byte, packetType PacketType) ([]byte, error)
}

// Statistics tracks TAP device I/O stats
type Statistics struct {
	BytesReceived   uint64
	BytesSent       uint64
	PacketsReceived uint64
	PacketsSent     uint64
	Errors          uint64
	LastErrorTime   time.Time
	LastError       string
	mutex           sync.RWMutex
}

// VPNDevice extends the Device struct with VPN-specific functionality
type VPNDevice struct {
	*Device
	Stats          Statistics
	processors     []PacketProcessor
	processorMutex sync.RWMutex
	closed         atomic.Bool
	packetPool     sync.Pool
	mtu            int
}

// NewVPNDevice creates a VPN-specific TAP device
func NewVPNDevice(config Config) (*VPNDevice, error) {
	// Force TAP mode for VPN
	config.DevType = TAP

	// Create the underlying device
	dev, err := Create(config)
	if err != nil {
		return nil, err
	}

	vpnDev := &VPNDevice{
		Device: dev,
		mtu:    1500, // Default MTU
		packetPool: sync.Pool{
			New: func() interface{} {
				// Buffer size large enough for MTU + Ethernet header
				return make([]byte, 1500+14)
			},
		},
	}

	return vpnDev, nil
}

// RegisterProcessor adds a packet processor to the processing chain
func (v *VPNDevice) RegisterProcessor(processor PacketProcessor) {
	v.processorMutex.Lock()
	defer v.processorMutex.Unlock()
	v.processors = append(v.processors, processor)
}

// ParseEthernetHeader parses an Ethernet header from a packet
func ParseEthernetHeader(packet []byte) (EthernetHeader, PacketType, error) {
	if len(packet) < 14 {
		return EthernetHeader{}, PacketTypeUnknown, ErrPacketTooShort
	}

	var header EthernetHeader
	copy(header.Destination[:], packet[0:6])
	copy(header.Source[:], packet[6:12])
	header.EtherType = binary.BigEndian.Uint16(packet[12:14])

	var packetType PacketType
	switch header.EtherType {
	case EtherTypeIPv4:
		packetType = PacketTypeIPv4
	case EtherTypeIPv6:
		packetType = PacketTypeIPv6
	case EtherTypeARP:
		packetType = PacketTypeARP
	default:
		packetType = PacketTypeUnknown
	}

	return header, packetType, nil
}

// StartPacketProcessing starts a goroutine that reads packets
// from the TAP device and processes them
func (v *VPNDevice) StartPacketProcessing() {
	v.closed.Store(false)

	go func() {
		for {
			if v.closed.Load() {
				return
			}

			buf := v.packetPool.Get().([]byte)
			n, err := v.Read(buf)

			if err != nil {
				v.Stats.mutex.Lock()
				v.Stats.Errors++
				v.Stats.LastError = err.Error()
				v.Stats.LastErrorTime = time.Now()
				v.Stats.mutex.Unlock()
				v.packetPool.Put(buf)
				// Use a small delay to prevent tight loops on persistent errors
				time.Sleep(5 * time.Millisecond)
				continue
			}

			if n > 0 {
				atomic.AddUint64(&v.Stats.PacketsReceived, 1)
				atomic.AddUint64(&v.Stats.BytesReceived, uint64(n))

				// Process in a new goroutine to keep reading
				packet := buf[:n]
				go v.processPacket(packet)
			} else {
				v.packetPool.Put(buf)
			}
		}
	}()
}

// processPacket processes a single packet through all registered processors
func (v *VPNDevice) processPacket(packet []byte) {
	defer v.packetPool.Put(packet)

	header, packetType, err := ParseEthernetHeader(packet)
	if err != nil {
		// Log or handle malformed packets
		return
	}

	v.processorMutex.RLock()
	processors := v.processors
	v.processorMutex.RUnlock()

	var processedPacket []byte = packet
	var processingErr error

	// Run through all processors
	for _, processor := range processors {
		processedPacket, processingErr = processor.ProcessPacket(processedPacket, packetType)
		if processingErr != nil {
			// Log or handle processing errors
			return
		}

		// Processor might signal to drop packet by returning nil
		if processedPacket == nil {
			return
		}
	}

	// Write the processed packet back to the TAP device
	n, err := v.Write(processedPacket)
	if err != nil {
		v.Stats.mutex.Lock()
		v.Stats.Errors++
		v.Stats.LastError = err.Error()
		v.Stats.LastErrorTime = time.Now()
		v.Stats.mutex.Unlock()
		return
	}

	atomic.AddUint64(&v.Stats.PacketsSent, 1)
	atomic.AddUint64(&v.Stats.BytesSent, uint64(n))
}

// StopPacketProcessing stops the packet processing goroutine
func (v *VPNDevice) StopPacketProcessing() {
	v.closed.Store(true)
}

// Close stops packet processing and closes the underlying device
func (v *VPNDevice) Close() error {
	v.StopPacketProcessing()
	return v.Device.Close()
}

// Implementing common VPN-specific packet processors

// EncryptionProcessor encrypts and decrypts packets
type EncryptionProcessor struct {
	encrypt func([]byte) ([]byte, error)
	decrypt func([]byte) ([]byte, error)
}

// NewEncryptionProcessor creates a new encryption processor
func NewEncryptionProcessor(encrypt, decrypt func([]byte) ([]byte, error)) *EncryptionProcessor {
	return &EncryptionProcessor{
		encrypt: encrypt,
		decrypt: decrypt,
	}
}

// ProcessPacket implements the PacketProcessor interface
func (e *EncryptionProcessor) ProcessPacket(packet []byte, packetType PacketType) ([]byte, error) {
	// Implementation would depend on the VPN protocol
	// This is a simplified example
	return e.encrypt(packet)
}

// FirewallProcessor implements basic firewall functionality
type FirewallProcessor struct {
	allowedIPs []net.IPNet
	mutex      sync.RWMutex
}

// NewFirewallProcessor creates a new firewall processor
func NewFirewallProcessor() *FirewallProcessor {
	return &FirewallProcessor{
		allowedIPs: make([]net.IPNet, 0),
	}
}

// AddAllowedNetwork adds a network to the allowed list
func (f *FirewallProcessor) AddAllowedNetwork(network string) error {
	_, ipNet, err := net.ParseCIDR(network)
	if err != nil {
		return err
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.allowedIPs = append(f.allowedIPs, *ipNet)
	return nil
}

// ProcessPacket implements the PacketProcessor interface
func (f *FirewallProcessor) ProcessPacket(packet []byte, packetType PacketType) ([]byte, error) {
	// Extract IP addresses from packet and check against allowed networks
	// This is a simplified example - a real implementation would parse the IP header
	return packet, nil
}

// Errors
var (
	ErrPacketTooShort = errors.New("packet too short")
)

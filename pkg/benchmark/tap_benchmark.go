package benchmark

import (
	"fmt"
	"n2n-go/pkg/log"
	"n2n-go/pkg/tuntap"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Debug flag for verbose logging
var DEBUG = false

// TAPBenchmarkOptions configures the TAP benchmark
type TAPBenchmarkOptions struct {
	TAPInterface1 string        // Name for first TAP interface
	TAPInterface2 string        // Name for second TAP interface
	IP1           string        // IP for first interface (CIDR notation)
	IP2           string        // IP for second interface (CIDR notation)
	PacketSize    int           // Size of test packets
	Iterations    int           // Number of packets to send
	UseBridge     bool          // Whether to create a bridge between interfaces
	BridgeName    string        // Name of the bridge (if used)
	Timeout       time.Duration // Timeout for waiting for each packet response
	MaxDuration   time.Duration // Maximum total benchmark duration
}

// DefaultTAPBenchmarkOptions returns sensible defaults
func DefaultTAPBenchmarkOptions() *TAPBenchmarkOptions {
	return &TAPBenchmarkOptions{
		TAPInterface1: "n2ntest0",
		TAPInterface2: "n2ntest1",
		IP1:           "10.0.0.1/24",
		IP2:           "10.0.0.2/24",
		PacketSize:    1024,
		Iterations:    1000,
		UseBridge:     true,
		BridgeName:    "n2nbridge",
		Timeout:       1000 * time.Millisecond, // Per-packet timeout
		MaxDuration:   2 * time.Minute,         // Overall benchmark timeout
	}
}

// Debug logging
func debugLog(format string, args ...interface{}) {
	if DEBUG {
		log.Printf("DEBUG: "+format, args...)
	}
}

// BenchmarkTAP performs a benchmark of TAP device performance
func BenchmarkTAP(opts *TAPBenchmarkOptions) (*LatencyResults, error) {
	log.Printf("Starting TAP benchmark with %d iterations, %d bytes",
		opts.Iterations, opts.PacketSize)

	// Create and set up a context with timeout for the entire benchmark
	benchmarkDeadline := time.Now().Add(opts.MaxDuration)
	log.Printf("Benchmark will run until %s", benchmarkDeadline.Format(time.TimeOnly))

	// Create TAP interfaces
	debugLog("Creating first TAP interface: %s", opts.TAPInterface1)
	tap1, err := tuntap.NewInterface(opts.TAPInterface1, "tap")
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %w",
			opts.TAPInterface1, err)
	}
	defer tap1.Close()
	defer cleanupInterface(opts.TAPInterface1)

	debugLog("Creating second TAP interface: %s", opts.TAPInterface2)
	tap2, err := tuntap.NewInterface(opts.TAPInterface2, "tap")
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %w",
			opts.TAPInterface2, err)
	}
	defer tap2.Close()
	defer cleanupInterface(opts.TAPInterface2)

	// Configure interfaces
	debugLog("Configuring %s with IP %s", opts.TAPInterface1, opts.IP1)
	if err := configureInterface(opts.TAPInterface1, opts.IP1); err != nil {
		return nil, fmt.Errorf("failed to configure %s: %w", opts.TAPInterface1, err)
	}

	debugLog("Configuring %s with IP %s", opts.TAPInterface2, opts.IP2)
	if err := configureInterface(opts.TAPInterface2, opts.IP2); err != nil {
		return nil, fmt.Errorf("failed to configure %s: %w", opts.TAPInterface2, err)
	}

	// Create bridge if requested
	if opts.UseBridge {
		debugLog("Creating bridge %s connecting %s and %s",
			opts.BridgeName, opts.TAPInterface1, opts.TAPInterface2)
		if err := createBridge(opts.BridgeName,
			[]string{opts.TAPInterface1, opts.TAPInterface2}); err != nil {
			return nil, fmt.Errorf("failed to create bridge: %w", err)
		}
		defer cleanupBridge(opts.BridgeName)

		// Verify bridge creation
		if output, err := exec.Command("ip", "link", "show", opts.BridgeName).CombinedOutput(); err != nil {
			log.Printf("Error verifying bridge: %v", err)
			log.Printf("Command output: %s", string(output))
		} else {
			debugLog("Bridge verified: %s", string(output))
		}

		// Check bridge interfaces
		if output, err := exec.Command("bridge", "link", "show").CombinedOutput(); err != nil {
			log.Printf("Error checking bridge links: %v", err)
		} else {
			debugLog("Bridge links: %s", string(output))
		}
	}

	// Wait for network to stabilize
	debugLog("Waiting for network to stabilize...")
	time.Sleep(500 * time.Millisecond)

	// Try a ping to verify connectivity
	debugLog("Testing connectivity with ping...")
	ip1 := opts.IP1
	if idx := strings.Index(ip1, "/"); idx > 0 {
		ip1 = ip1[:idx]
	}

	ip2 := opts.IP2
	if idx := strings.Index(ip2, "/"); idx > 0 {
		ip2 = ip2[:idx]
	}

	/*pingCmd := exec.Command("ping", "-c", "1", "-W", "1", ip2)
	pingCmd.Env = append(pingCmd.Env, fmt.Sprintf("LANG=C"))
	if output, err := pingCmd.CombinedOutput(); err != nil {
		log.Printf("Ping test failed: %v", err)
		log.Printf("Ping output: %s", string(output))
	} else {
		debugLog("Ping test successful: %s", string(output))
	}*/

	// Prepare packet buffers
	sendBuf := make([]byte, opts.PacketSize)

	// Get MAC addresses for debugging
	mac1 := tap1.HardwareAddr()
	mac2 := tap2.HardwareAddr()

	debugLog("TAP1 MAC: %s, TAP2 MAC: %s", mac1, mac2)

	// Create an IPv4 ping packet
	//debugLog("Creating ping packet from %s to %s", ip1, ip2)
	createPingPacket(sendBuf, mac1, ip1, ip2, 1)
	debugPrintPacket(sendBuf, opts.PacketSize)

	// Start the responder and clean up when done
	stopResponder := make(chan struct{})
	var responderWg sync.WaitGroup
	responderWg.Add(1)

	debugLog("Starting responder on %s", opts.TAPInterface2)
	go func() {
		defer responderWg.Done()
		runResponder(tap2, stopResponder)
	}()

	defer func() {
		debugLog("Stopping responder...")
		close(stopResponder)
		responderWg.Wait()
	}()

	// Wait a moment to see if we get any response
	//time.Sleep(100 * time.Millisecond)

	// Test the responder with a single packet
	/*debugLog("Sending test packet to ensure responder is working...")
	_, err = tap1.Write(sendBuf)
	if err != nil {
		log.Printf("Error sending test packet: %v", err)
	} else {
		debugLog("Test packet sent successfully")
	}

	// Wait a moment to see if we get any response
	time.Sleep(200 * time.Millisecond)
	*/
	// Collect results
	var latencies []time.Duration
	var latenciesMu sync.Mutex
	addLatency := func(rtt time.Duration) {
		latenciesMu.Lock()
		latencies = append(latencies, rtt)
		latenciesMu.Unlock()
	}

	// Track packet statistics
	sent := 0
	received := 0

	// Run the benchmark
	startTime := time.Now()

	// First, specially create our packet with sequence 0
	/*	updateICMPSequence(sendBuf, uint16(0))
		updateICMPChecksum(sendBuf)
		debugLog("First packet using sequence number 0")
	*/
	// Send packets and measure RTT
	for i := 0; i < opts.Iterations && time.Now().Before(benchmarkDeadline); i++ {
		// Update sequence number correctly - using i as the sequence number
		// This ensures we're looking for the same sequence number we're sending
		updateICMPSequence(sendBuf, uint16(i))
		updateICMPChecksum(sendBuf)

		debugLog("Sending packet with sequence %d", i)

		sent++
		sendTime := time.Now()

		// Send packet
		_, err = tap1.Write(sendBuf)
		if err != nil {
			log.Printf("Error writing to %s: %v", opts.TAPInterface1, err)
			continue
		}

		// Read response with timeout
		responseReceived := make(chan bool, 1)

		func(seq uint16, sentAt time.Time) {
			recvBuf := make([]byte, opts.PacketSize*2)
			//_ = tap1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := tap1.Read(recvBuf)
			fmt.Println("\n\\n\n\n\n", n)

			if err != nil {
				debugLog("Error reading response for packet %d: %v", seq, err)
				responseReceived <- false
				return
			}

			// Very important: Check for exactly the sequence number we sent
			if isICMPEchoReply(recvBuf[:n], seq) {
				debugLog("Received valid echo reply for packet %d", seq)
				rtt := time.Since(sentAt)
				addLatency(rtt)
				received++
				responseReceived <- true
			} else {
				debugLog("Packet didn't match expected sequence %d - dumping for analysis", seq)
				debugDumpFullPacket(recvBuf[:n], fmt.Sprintf("Rejected Packet (seq=%d)", seq))
				responseReceived <- false
			}
		}(uint16(i), sendTime)

		// Wait for response or timeout
		select {
		case success := <-responseReceived:
			if success {
				debugLog("Successfully processed packet %d", i)
			}
		case <-time.After(opts.Timeout):
			debugLog("Timeout waiting for response to packet %d", i)
		}

		// Small delay between packets to prevent flooding
		time.Sleep(5 * time.Millisecond)
	}
	// Calculate total time
	totalTime := time.Since(startTime)

	// Report detailed statistics
	debugLog("Benchmark complete - sent: %d, received: %d, loss: %.2f%%",
		sent, received, 100.0*(float64(sent-received)/float64(sent)))

	// Calculate statistics
	results := calculateStats(latencies, opts.Iterations, totalTime)
	results.PacketSize = opts.PacketSize
	results.Component = ComponentTAPOnly
	results.PacketsSent = sent
	results.PacketsRecv = received

	log.Printf("TAP benchmark complete: %d packets sent, %d received (%.2f%% loss)",
		sent, received, 100.0*(float64(sent-received)/float64(sent)))

	return results, nil
}

// debugPrintPacket prints a packet in hex dump format
func debugPrintPacket(buf []byte, maxLen int) {
	if !DEBUG {
		return
	}

	if len(buf) > maxLen {
		buf = buf[:maxLen]
	}

	// Print Ethernet header
	log.Printf("Ethernet Header:")
	log.Printf(" Dest MAC: %02x:%02x:%02x:%02x:%02x:%02x",
		buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	log.Printf(" Src MAC:  %02x:%02x:%02x:%02x:%02x:%02x",
		buf[6], buf[7], buf[8], buf[9], buf[10], buf[11])
	log.Printf(" EtherType: 0x%02x%02x", buf[12], buf[13])

	// Print IP header
	log.Printf("IP Header:")
	log.Printf(" Version/IHL: 0x%02x", buf[14])
	log.Printf(" ToS: 0x%02x", buf[15])
	log.Printf(" Total Length: %d", uint16(buf[16])<<8|uint16(buf[17]))
	log.Printf(" ID: 0x%02x%02x", buf[18], buf[19])
	log.Printf(" Flags/Fragment: 0x%02x%02x", buf[20], buf[21])
	log.Printf(" TTL: %d", buf[22])
	log.Printf(" Protocol: %d", buf[23])
	log.Printf(" Checksum: 0x%02x%02x", buf[24], buf[25])
	log.Printf(" Source IP: %d.%d.%d.%d", buf[26], buf[27], buf[28], buf[29])
	log.Printf(" Dest IP: %d.%d.%d.%d", buf[30], buf[31], buf[32], buf[33])

	// Print ICMP header
	log.Printf("ICMP Header:")
	log.Printf(" Type: %d", buf[34])
	log.Printf(" Code: %d", buf[35])
	log.Printf(" Checksum: 0x%02x%02x", buf[36], buf[37])
	log.Printf(" Identifier: 0x%02x%02x", buf[38], buf[39])
	log.Printf(" Sequence: %d", uint16(buf[40])<<8|uint16(buf[41]))
}

// createPingPacket creates a complete Ethernet+IPv4+ICMP echo request packet
func createPingPacket(buf []byte, srcMAC net.HardwareAddr, srcIP, dstIP string, seq uint16) {
	// We need to parse the IP addresses
	srcIPAddr := net.ParseIP(srcIP).To4()
	dstIPAddr := net.ParseIP(dstIP).To4()

	if srcIPAddr == nil || dstIPAddr == nil {
		log.Fatalf("Invalid IP addresses: src=%s, dst=%s", srcIP, dstIP)
	}

	// 1. Ethernet header (14 bytes)
	// Destination MAC (broadcast)
	for i := 0; i < 6; i++ {
		buf[i] = 0xFF
	}

	// Source MAC
	copy(buf[6:12], srcMAC)

	// EtherType (IPv4 = 0x0800)
	buf[12] = 0x08
	buf[13] = 0x00

	// 2. IPv4 header (20 bytes)
	ipStart := 14

	// Version (4) and IHL (5 words = 20 bytes): 0x45
	buf[ipStart] = 0x45
	// DSCP/ECN: 0
	buf[ipStart+1] = 0x00

	// Total length (IP + ICMP): to be set below
	ipTotalLen := 20 + 8 + (len(buf) - (14 + 20 + 8))
	buf[ipStart+2] = byte(ipTotalLen >> 8)
	buf[ipStart+3] = byte(ipTotalLen & 0xFF)

	// Identification: 0x1234 (arbitrary)
	buf[ipStart+4] = 0x12
	buf[ipStart+5] = 0x34

	// Flags (0) and Fragment Offset (0)
	buf[ipStart+6] = 0x00
	buf[ipStart+7] = 0x00

	// TTL: 64
	buf[ipStart+8] = 64
	// Protocol: 1 (ICMP)
	buf[ipStart+9] = 0x01

	// Header checksum: to be calculated later
	buf[ipStart+10] = 0x00
	buf[ipStart+11] = 0x00

	// Source IP
	copy(buf[ipStart+12:ipStart+16], srcIPAddr)

	// Destination IP
	copy(buf[ipStart+16:ipStart+20], dstIPAddr)

	// 3. ICMP header (8+ bytes)
	icmpStart := ipStart + 20

	// Type: 8 (Echo Request)
	buf[icmpStart] = 0x08
	// Code: 0
	buf[icmpStart+1] = 0x00
	// Checksum: to be calculated later
	buf[icmpStart+2] = 0x00
	buf[icmpStart+3] = 0x00

	// Identifier: 0x4242 (arbitrary)
	buf[icmpStart+4] = 0x42
	buf[icmpStart+5] = 0x42

	// Sequence number
	buf[icmpStart+6] = byte(seq >> 8)
	buf[icmpStart+7] = byte(seq & 0xFF)

	// 4. Payload (rest of the packet)
	// Fill with pattern
	for i := icmpStart + 8; i < len(buf); i++ {
		buf[i] = byte(i % 256)
	}

	// 5. Calculate IP header checksum
	ipChecksum := calculateChecksum(buf[ipStart : ipStart+20])
	buf[ipStart+10] = byte(ipChecksum >> 8)
	buf[ipStart+11] = byte(ipChecksum & 0xFF)

	// 6. Calculate ICMP checksum
	icmpChecksum := calculateChecksum(buf[icmpStart:])
	buf[icmpStart+2] = byte(icmpChecksum >> 8)
	buf[icmpStart+3] = byte(icmpChecksum & 0xFF)
}

// Rest of the functions (updateICMPSequence, updateICMPChecksum, etc.) remain the same
// updateICMPSequence updates the sequence number in an existing ICMP packet
func updateICMPSequence(buf []byte, seq uint16) {
	// Find ICMP header (Ethernet + IPv4 header)
	icmpStart := 14 + 20

	// Update sequence number
	buf[icmpStart+6] = byte(seq >> 8)
	buf[icmpStart+7] = byte(seq & 0xFF)
}

// updateICMPChecksum recalculates the ICMP checksum after changes
func updateICMPChecksum(buf []byte) {
	// Find ICMP header (Ethernet + IPv4 header)
	icmpStart := 14 + 20

	// Zero out current checksum
	buf[icmpStart+2] = 0x00
	buf[icmpStart+3] = 0x00

	// Calculate new checksum
	icmpChecksum := calculateChecksum(buf[icmpStart:])
	buf[icmpStart+2] = byte(icmpChecksum >> 8)
	buf[icmpStart+3] = byte(icmpChecksum & 0xFF)
}

// isICMPEchoRequest checks if a packet is an ICMP echo request
func isICMPEchoRequest(buf []byte) bool {
	// Check minimum size
	if len(buf) < 14+20+8 {
		return false
	}

	// Check EtherType (IPv4)
	if buf[12] != 0x08 || buf[13] != 0x00 {
		return false
	}

	// Check IPv4 protocol (ICMP)
	if buf[14+9] != 0x01 {
		return false
	}

	// Check ICMP type (Echo Request)
	if buf[14+20] != 0x08 {
		return false
	}

	return true
}

// convertToEchoReply converts an ICMP echo request to an echo reply
func convertToEchoReply(buf []byte) {
	// Find ICMP header
	icmpStart := 14 + 20

	// Store original source/destination MACs and IPs for swapping
	srcMAC := make([]byte, 6)
	dstMAC := make([]byte, 6)
	srcIP := make([]byte, 4)
	dstIP := make([]byte, 4)

	// Save original values
	copy(srcMAC, buf[6:12])
	copy(dstMAC, buf[0:6])
	copy(srcIP, buf[14+12:14+16])
	copy(dstIP, buf[14+16:14+20])

	// Swap source and destination MAC addresses
	copy(buf[0:6], srcMAC)
	copy(buf[6:12], dstMAC)

	// Swap source and destination IP addresses
	copy(buf[14+12:14+16], dstIP)
	copy(buf[14+16:14+20], srcIP)

	// Change ICMP type from 8 (echo request) to 0 (echo reply)
	buf[icmpStart] = 0x00

	// Code should remain 0
	buf[icmpStart+1] = 0x00

	// Important: Keep the same identifier and sequence number!
	// We don't need to modify bytes icmpStart+4 through icmpStart+7

	// Recalculate IP header checksum
	// Zero out the old checksum
	buf[14+10] = 0x00
	buf[14+11] = 0x00

	// Calculate new checksum
	ipChecksum := calculateChecksum(buf[14 : 14+20])
	buf[14+10] = byte(ipChecksum >> 8)
	buf[14+11] = byte(ipChecksum & 0xFF)

	// Recalculate ICMP checksum
	// Zero out the old checksum
	buf[icmpStart+2] = 0x00
	buf[icmpStart+3] = 0x00

	// Calculate new checksum
	icmpLen := len(buf) - icmpStart
	icmpChecksum := calculateChecksum(buf[icmpStart : icmpStart+icmpLen])
	buf[icmpStart+2] = byte(icmpChecksum >> 8)
	buf[icmpStart+3] = byte(icmpChecksum & 0xFF)

	debugLog("Converted Echo Request to Reply: ID=0x%02x%02x, Seq=%d",
		buf[icmpStart+4], buf[icmpStart+5],
		uint16(buf[icmpStart+6])<<8|uint16(buf[icmpStart+7]))
}

// calculateChecksum calculates the Internet Checksum of a byte slice
func calculateChecksum(data []byte) uint16 {
	var sum uint32

	// Sum all 16-bit words
	for i := 0; i < len(data); i += 2 {
		if i+1 < len(data) {
			sum += uint32(data[i])<<8 | uint32(data[i+1])
		} else {
			sum += uint32(data[i]) << 8
		}
	}

	// Add back carry bits
	for sum>>16 > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}

	// Take one's complement
	return ^uint16(sum)
}

// debugDumpFullPacket does a more comprehensive dump of a packet for debugging
func debugDumpFullPacket(buf []byte, label string) {
	if !DEBUG {
		return
	}

	log.Printf("--- PACKET DUMP [%s] (%d bytes) ---", label, len(buf))

	// Dump as hex
	log.Printf("Raw hex:")
	for i := 0; i < len(buf); i += 16 {
		end := i + 16
		if end > len(buf) {
			end = len(buf)
		}

		// Print hex values
		hexLine := "  "
		for j := i; j < end; j++ {
			hexLine += fmt.Sprintf("%02x ", buf[j])
		}

		// Pad remaining space if not a full line
		for j := end; j < i+16; j++ {
			hexLine += "   "
		}

		// Print ASCII representation
		hexLine += " | "
		for j := i; j < end; j++ {
			if buf[j] >= 32 && buf[j] <= 126 {
				hexLine += string(buf[j])
			} else {
				hexLine += "."
			}
		}

		log.Print(hexLine)
	}

	// Try to interpret as Ethernet + IP
	if len(buf) >= 14 {
		log.Printf("Ethernet Header:")
		log.Printf(" Dest MAC: %02x:%02x:%02x:%02x:%02x:%02x",
			buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
		log.Printf(" Src MAC:  %02x:%02x:%02x:%02x:%02x:%02x",
			buf[6], buf[7], buf[8], buf[9], buf[10], buf[11])
		log.Printf(" EtherType: 0x%02x%02x", buf[12], buf[13])

		// Check for IPv4
		if buf[12] == 0x08 && buf[13] == 0x00 && len(buf) >= 34 {
			log.Printf("IPv4 Header:")
			log.Printf(" Version/IHL: 0x%02x", buf[14])
			log.Printf(" ToS: 0x%02x", buf[15])
			totalLen := uint16(buf[16])<<8 | uint16(buf[17])
			log.Printf(" Total Length: %d", totalLen)
			log.Printf(" ID: 0x%02x%02x", buf[18], buf[19])
			log.Printf(" Flags/Fragment: 0x%02x%02x", buf[20], buf[21])
			log.Printf(" TTL: %d", buf[22])
			log.Printf(" Protocol: %d", buf[23])
			log.Printf(" Checksum: 0x%02x%02x", buf[24], buf[25])
			log.Printf(" Source IP: %d.%d.%d.%d", buf[26], buf[27], buf[28], buf[29])
			log.Printf(" Dest IP: %d.%d.%d.%d", buf[30], buf[31], buf[32], buf[33])

			// Check for ICMP
			if buf[23] == 1 && len(buf) >= 42 {
				log.Printf("ICMP Header:")
				log.Printf(" Type: %d", buf[34])
				log.Printf(" Code: %d", buf[35])
				log.Printf(" Checksum: 0x%02x%02x", buf[36], buf[37])
				log.Printf(" Identifier: 0x%02x%02x", buf[38], buf[39])
				seq := uint16(buf[40])<<8 | uint16(buf[41])
				log.Printf(" Sequence: %d", seq)
			}
		}

		// Check for IPv6
		if buf[12] == 0x86 && buf[13] == 0xDD && len(buf) >= 54 {
			log.Printf("IPv6 Header Detected - these packets are normal neighbor discovery")
		}
	}

	log.Printf("--- END PACKET DUMP ---")
}

// min returns the smaller of x or y
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// runResponder runs a responder that replies to ICMP echo requests
func runResponder(tap *tuntap.Interface, stopChan <-chan struct{}) {
	debugLog("Responder started")
	packetsReceived := 0
	packetsReplied := 0

	// Get our MAC address for proper replies
	ourMAC := tap.HardwareAddr()
	debugLog("Responder MAC address: %s", formatMAC(ourMAC))

	for {
		select {
		case <-stopChan:
			debugLog("Responder stopping after handling %d packets (%d replies)",
				packetsReceived, packetsReplied)
			return
		default:
			// Continue with non-blocking behavior
		}

		// Read with short timeout using a goroutine
		recvBuf := make([]byte, 2048) // Large enough for any packet
		readDone := make(chan int, 1)

		//go func() {
		n, err := tap.Read(recvBuf)
		if err != nil {
			readDone <- 0
			return
		} else {
			readDone <- n
		}
		//}()

		// Wait for read or timeout
		var na int
		select {
		case <-stopChan:
			debugLog("Responder stopping during read")
			return

		case <-time.After(1000 * time.Millisecond):
			// Short timeout, try again
			continue

		case na = <-readDone:
			if na == 0 {
				// Read error
				continue
			}
		}

		// Process the packet
		packetsReceived++

		// IPv6 neighbor discovery packets can be ignored
		if n >= 14 && recvBuf[12] == 0x86 && recvBuf[13] == 0xDD {
			if packetsReceived%10 == 0 { // Only log occasionally to reduce noise
				debugLog("Responder ignoring IPv6 packet #%d", packetsReceived)
			}
			continue
		}

		if isICMPEchoRequest(recvBuf[:n]) {
			debugLog("Responder received echo request (packet #%d)", packetsReceived)
			debugDumpFullPacket(recvBuf[:n], "Echo Request Received")

			// Create response packet (echo reply)
			respBuf := make([]byte, n)
			copy(respBuf, recvBuf[:n])

			// Convert ping request to ping reply, passing our MAC for proper reply
			convertToEchoReplyWithMAC(respBuf, ourMAC)

			// Dump the reply packet for debugging
			debugDumpFullPacket(respBuf, "Echo Reply Sending")

			// Send response
			_, err := tap.Write(respBuf)
			if err != nil {
				debugLog("Error sending echo reply: %v", err)
			} else {
				packetsReplied++
				debugLog("Responder sent echo reply #%d", packetsReplied)
			}
		} else {
			if n >= 14 && recvBuf[12] == 0x08 && recvBuf[13] == 0x00 {
				debugLog("Responder received non-ICMP-echo IPv4 packet (size: %d)", n)
				debugDumpFullPacket(recvBuf[:n], "Non-Echo IPv4 Packet")
			} else {
				debugLog("Responder received unknown packet type (size: %d)", n)
				// Only dump the first 20 bytes to avoid log spam
				debugLog("Packet data: %X", recvBuf[:min(20, n)])
			}
		}
	}
}

// convertToEchoReplyWithMAC converts an ICMP echo request to an echo reply
// with the specified MAC address as source
func convertToEchoReplyWithMAC(buf []byte, ourMAC net.HardwareAddr) {
	// Find ICMP header
	icmpStart := 14 + 20

	// Get the source MAC from the request - this will be our destination MAC
	srcMAC := make([]byte, 6)
	copy(srcMAC, buf[6:12])

	// Set destination MAC in reply = Source MAC from request
	copy(buf[0:6], srcMAC)

	// Set source MAC in reply = Our MAC address
	copy(buf[6:12], ourMAC)

	// Swap source and destination IP addresses
	srcIP := make([]byte, 4)
	dstIP := make([]byte, 4)
	copy(srcIP, buf[14+12:14+16]) // Source IP in the request
	copy(dstIP, buf[14+16:14+20]) // Destination IP in the request

	// Destination IP in reply = Source IP from request
	copy(buf[14+16:14+20], srcIP)

	// Source IP in reply = Destination IP from request
	copy(buf[14+12:14+16], dstIP)

	// Change ICMP type from 8 (echo request) to 0 (echo reply)
	buf[icmpStart] = 0x00

	// Recalculate IP header checksum
	// Zero out the old checksum
	buf[14+10] = 0x00
	buf[14+11] = 0x00

	// Calculate new checksum
	ipChecksum := calculateChecksum(buf[14 : 14+20])
	buf[14+10] = byte(ipChecksum >> 8)
	buf[14+11] = byte(ipChecksum & 0xFF)

	// Recalculate ICMP checksum
	// Zero out the old checksum
	buf[icmpStart+2] = 0x00
	buf[icmpStart+3] = 0x00

	// Calculate new checksum
	icmpChecksum := calculateChecksum(buf[icmpStart:])
	buf[icmpStart+2] = byte(icmpChecksum >> 8)
	buf[icmpStart+3] = byte(icmpChecksum & 0xFF)

	debugLog("Converted Echo Request to Reply: ID=0x%02x%02x, Seq=%d, SrcMAC=%s",
		buf[icmpStart+4], buf[icmpStart+5],
		uint16(buf[icmpStart+6])<<8|uint16(buf[icmpStart+7]),
		formatMAC(buf[6:12]))
}

// formatMAC formats a MAC address as a string
func formatMAC(mac []byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// isICMPEchoReply checks if a packet is an ICMP echo reply with the expected sequence
func isICMPEchoReply(buf []byte, expectedSeq uint16) bool {
	// Check minimum size - Ethernet(14) + IPv4(20) + ICMP(8)
	if len(buf) < 14+20+8 {
		debugLog("Packet too short: %d bytes", len(buf))
		return false
	}

	// Check EtherType (IPv4 = 0x0800)
	if buf[12] != 0x08 || buf[13] != 0x00 {
		debugLog("Not IPv4 packet: ethertype 0x%02x%02x", buf[12], buf[13])
		return false
	}

	// Check IPv4 protocol (ICMP)
	if buf[14+9] != 0x01 {
		debugLog("Not ICMP packet: protocol %d", buf[14+9])
		return false
	}

	// Check ICMP type (Echo Reply = 0)
	if buf[14+20] != 0x00 {
		debugLog("Not Echo Reply: type %d", buf[14+20])
		return false
	}

	// Check ICMP code (should be 0)
	if buf[14+21] != 0x00 {
		debugLog("Wrong ICMP code: %d", buf[14+21])
		return false
	}

	// Check ICMP identifier (should be our test value 0x4242)
	icmpIdent := uint16(buf[14+20+4])<<8 | uint16(buf[14+20+5])
	if icmpIdent != 0x4242 {
		debugLog("Wrong ICMP identifier: 0x%04x, expected 0x4242", icmpIdent)
		return false
	}

	// Extract sequence number from the packet
	icmpStart := 14 + 20
	seq := uint16(buf[icmpStart+6])<<8 | uint16(buf[icmpStart+7])

	// Check if sequence matches what we expect
	if seq != expectedSeq {
		debugLog("Sequence mismatch: got %d, expected %d", seq, expectedSeq)
		return false
	}

	// Looks like a valid reply with the right sequence
	debugLog("Valid ICMP Echo Reply with sequence %d", seq)
	return true
}

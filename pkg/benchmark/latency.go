package benchmark

import (
	"fmt"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/edge"
	"n2n-go/pkg/log"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/spec"
	"n2n-go/pkg/supernode"
	"n2n-go/pkg/tuntap"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LatencyResults holds the results of a latency benchmark
type LatencyResults struct {
	MinLatency    time.Duration
	MaxLatency    time.Duration
	AvgLatency    time.Duration
	MedianLatency time.Duration
	P95Latency    time.Duration
	P99Latency    time.Duration
	PacketsSent   int
	PacketsRecv   int
	TotalTime     time.Duration
	PacketSize    int
	Component     Component
}

// Component specifies which component to benchmark
type Component int

const (
	ComponentAll          Component = iota // Complete end-to-end path
	ComponentUDPOnly                       // Just UDP socket performance
	ComponentTAPOnly                       // Just TAP device performance
	ComponentProtocolOnly                  // Just protocol serialization/deserialization
)

// ComponentName returns the name of the component
func (c Component) String() string {
	switch c {
	case ComponentAll:
		return "Complete Path"
	case ComponentUDPOnly:
		return "UDP Socket"
	case ComponentTAPOnly:
		return "TAP Device"
	case ComponentProtocolOnly:
		return "Protocol Serialization"
	default:
		return "Unknown"
	}
}

// BenchmarkOptions provides configuration for benchmarks
type BenchmarkOptions struct {
	Component      Component
	Iterations     int
	PacketSize     int
	SupernodeAddr  string
	Community      string
	TAPInterfaceID int
}

// DefaultBenchmarkOptions returns sensible defaults
func DefaultBenchmarkOptions() *BenchmarkOptions {
	return &BenchmarkOptions{
		Component:      ComponentUDPOnly,
		Iterations:     1000,
		PacketSize:     1024,
		SupernodeAddr:  "127.0.0.1:7777",
		Community:      "benchcommunity",
		TAPInterfaceID: 10,
	}
}

// BenchmarkLatency measures latency for a specific component
func BenchmarkLatency(opts *BenchmarkOptions) (*LatencyResults, error) {
	switch opts.Component {
	case ComponentAll:
		return benchmarkEndToEnd(opts)
	case ComponentUDPOnly:
		return benchmarkUDPOnly(opts)
	case ComponentTAPOnly:
		return benchmarkTAPOnly(opts)
	case ComponentProtocolOnly:
		return benchmarkProtocolOnly(opts)
	default:
		return nil, fmt.Errorf("unknown component: %d", opts.Component)
	}
}

// benchmarkEndToEnd measures complete end-to-end latency
func benchmarkEndToEnd(opts *BenchmarkOptions) (*LatencyResults, error) {
	// This implementation creates a local supernode and two edges
	// and measures ping latency between them

	// 1. Start a supernode
	log.Printf("Starting temporary supernode...")
	snPort := 17777
	snAddr := fmt.Sprintf("127.0.0.1:%d", snPort)

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", snPort))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve supernode address: %w", err)
	}

	snConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on UDP for supernode: %w", err)
	}
	defer snConn.Close()

	snConfig := supernode.DefaultConfig()
	snConfig.Debug = false

	sn := supernode.NewSupernodeWithConfig(snConn, snConfig)

	go sn.Listen()
	defer sn.Shutdown()

	// 2. Create two tap interfaces for edges
	tap1Name := fmt.Sprintf("n2ntest%d", opts.TAPInterfaceID)
	tap2Name := fmt.Sprintf("n2ntest%d", opts.TAPInterfaceID+1)

	log.Printf("Creating TAP interfaces %s and %s...", tap1Name, tap2Name)

	// 3. Start Edge 1
	edge1Config := edge.Config{
		EdgeID:            "edge1",
		Community:         opts.Community,
		TapName:           tap1Name,
		LocalPort:         0,
		SupernodeAddr:     snAddr,
		HeartbeatInterval: 30 * time.Second,
	}
	edge1, err := edge.NewEdgeClient(edge1Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create edge1: %w", err)
	}
	defer edge1.Close()

	if err := edge1.InitialRegister(); err != nil {
		return nil, fmt.Errorf("failed to register edge1: %w", err)
	}

	// Configure interface for edge1
	if err := configureInterface(tap1Name, edge1.VirtualIP); err != nil {
		return nil, fmt.Errorf("failed to configure interface for edge1: %w", err)
	}

	go edge1.Run()

	// 4. Start Edge 2
	edge2Config := edge.Config{
		EdgeID:            "edge2",
		Community:         opts.Community,
		TapName:           tap2Name,
		LocalPort:         0,
		SupernodeAddr:     snAddr,
		HeartbeatInterval: 30 * time.Second,
	}
	edge2, err := edge.NewEdgeClient(edge2Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create edge2: %w", err)
	}
	defer edge2.Close()

	if err := edge2.InitialRegister(); err != nil {
		return nil, fmt.Errorf("failed to register edge2: %w", err)
	}

	// Configure interface for edge2
	if err := configureInterface(tap2Name, edge2.VirtualIP); err != nil {
		return nil, fmt.Errorf("failed to configure interface for edge2: %w", err)
	}

	go edge2.Run()

	// 5. Extract IP addresses for ping test
	ip1 := strings.Split(edge1.VirtualIP, "/")[0]
	ip2 := strings.Split(edge2.VirtualIP, "/")[0]

	log.Printf("Edge1 VIP: %s, Edge2 VIP: %s", ip1, ip2)

	// 6. Wait for interfaces to be ready
	time.Sleep(1 * time.Second)

	// 7. Ping from edge1 to edge2
	log.Printf("Starting ping test for %d iterations...", opts.Iterations)

	latencies, err := runPingTest(ip2, opts.Iterations, opts.PacketSize)
	if err != nil {
		return nil, fmt.Errorf("ping test failed: %w", err)
	}

	// 8. Calculate statistics
	results := calculateStats(latencies, opts.Iterations, latencies[0]*time.Duration(len(latencies)))
	results.PacketSize = opts.PacketSize
	results.Component = opts.Component

	return results, nil
}

// runPingTest uses the ping command to measure latency
func runPingTest(targetIP string, count, packetSize int) ([]time.Duration, error) {
	args := []string{
		"-c", strconv.Itoa(count),
		"-s", strconv.Itoa(packetSize),
		"-i", "0.01", // 10ms interval
		targetIP,
	}

	cmd := exec.Command("ping", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	// Parse ping output to extract latencies
	return parsePingOutput(string(output))
}

// parsePingOutput extracts latency values from ping command output
func parsePingOutput(output string) ([]time.Duration, error) {
	var latencies []time.Duration

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "time=") {
			parts := strings.Split(line, "time=")
			if len(parts) >= 2 {
				timeStr := strings.Split(parts[1], " ")[0]
				timeVal, err := strconv.ParseFloat(timeStr, 64)
				if err != nil {
					continue
				}

				// Convert milliseconds to nanoseconds
				latency := time.Duration(timeVal * float64(time.Millisecond))
				latencies = append(latencies, latency)
			}
		}
	}

	if len(latencies) == 0 {
		return nil, fmt.Errorf("no latency data found in ping output")
	}

	return latencies, nil
}

// configureInterface brings up a TAP interface with the given IP
func configureInterface(ifName, ipAddr string) error {
	/*// Create TAP interface if it doesn't exist
	cmd := exec.Command("ip", "tuntap", "add", "dev", ifName, "mode", "tap")
	if err := cmd.Run(); err != nil {
		// If it already exists, that's okay
		log.Printf("Note: TAP device creation returned: %v (might already exist)", err)
	}*/

	// Bring the interface up
	cmd := exec.Command("ip", "link", "set", "dev", ifName, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Assign IP address
	cmd = exec.Command("ip", "addr", "add", ipAddr, "dev", ifName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to assign IP address: %w", err)
	}

	return nil
}

// cleanupInterface removes a TAP interface
func cleanupInterface(ifName string) error {
	cmd := exec.Command("ip", "link", "delete", ifName)
	return cmd.Run()
}

// benchmarkUDPOnly measures pure UDP socket performance
func benchmarkUDPOnly(opts *BenchmarkOptions) (*LatencyResults, error) {
	// Create two UDP sockets to communicate with each other
	addr1, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve addr1: %w", err)
	}

	addr2, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve addr2: %w", err)
	}

	conn1, err := net.ListenUDP("udp", addr1)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on conn1: %w", err)
	}
	defer conn1.Close()

	conn2, err := net.ListenUDP("udp", addr2)
	if err != nil {
		conn1.Close()
		return nil, fmt.Errorf("failed to listen on conn2: %w", err)
	}
	defer conn2.Close()

	// Get actual addresses after binding
	//localAddr1 := conn1.LocalAddr().(*net.UDPAddr)
	localAddr2 := conn2.LocalAddr().(*net.UDPAddr)

	// Set larger buffer sizes
	conn1.SetReadBuffer(1024 * 1024)
	conn1.SetWriteBuffer(1024 * 1024)
	conn2.SetReadBuffer(1024 * 1024)
	conn2.SetWriteBuffer(1024 * 1024)

	// Set a reasonable deadline
	deadline := time.Now().Add(1 * time.Minute)
	conn1.SetReadDeadline(deadline)
	conn2.SetReadDeadline(deadline)

	// Prepare buffers with the specified packet size
	sendBuf := make([]byte, opts.PacketSize)
	recvBuf := make([]byte, opts.PacketSize)

	var wg sync.WaitGroup
	var latencies []time.Duration
	var latenciesMu sync.Mutex

	// Start receiver goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < opts.Iterations; i++ {
			n, addr, err := conn2.ReadFromUDP(recvBuf)
			if err != nil {
				log.Printf("Error reading from conn2: %v", err)
				continue
			}

			// Echo back
			_, err = conn2.WriteToUDP(recvBuf[:n], addr)
			if err != nil {
				log.Printf("Error writing to conn2: %v", err)
			}
		}
	}()

	// Send and measure round trip time
	startTime := time.Now()

	for i := 0; i < opts.Iterations; i++ {
		// Send packet
		sendTime := time.Now()
		_, err := conn1.WriteToUDP(sendBuf, localAddr2)
		if err != nil {
			log.Printf("Error writing to conn1: %v", err)
			continue
		}

		// Receive echo
		_, _, err = conn1.ReadFromUDP(recvBuf)
		if err != nil {
			log.Printf("Error reading from conn1: %v", err)
			continue
		}

		// Calculate latency
		rtt := time.Since(sendTime)

		latenciesMu.Lock()
		latencies = append(latencies, rtt)
		latenciesMu.Unlock()
	}

	totalTime := time.Since(startTime)

	// Wait for receiver to finish
	wg.Wait()

	// Calculate statistics
	results := calculateStats(latencies, opts.Iterations, totalTime)
	results.PacketSize = opts.PacketSize
	results.Component = opts.Component

	return results, nil
}

// benchmarkTAPOnly measures TAP device read/write performance
func benchmarkTAPOnly(opts *BenchmarkOptions) (*LatencyResults, error) {
	// Create two TAP devices
	tap1Name := fmt.Sprintf("n2ntest%d", opts.TAPInterfaceID)
	tap2Name := fmt.Sprintf("n2ntest%d", opts.TAPInterfaceID+1)

	log.Printf("Creating TAP interfaces %s and %s...", tap1Name, tap2Name)

	// Create and configure TAP interfaces
	tap1, err := tuntap.NewInterface(tap1Name, "tap")
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %w", tap1Name, err)
	}
	defer tap1.Close()
	defer cleanupInterface(tap1Name)

	tap2, err := tuntap.NewInterface(tap2Name, "tap")
	if err != nil {
		return nil, fmt.Errorf("failed to create TAP interface %s: %w", tap2Name, err)
	}
	defer tap2.Close()
	defer cleanupInterface(tap2Name)

	// Configure IP addresses
	ip1 := "10.0.0.1/24"
	ip2 := "10.0.0.2/24"

	if err := configureInterface(tap1Name, ip1); err != nil {
		return nil, fmt.Errorf("failed to configure %s: %w", tap1Name, err)
	}

	if err := configureInterface(tap2Name, ip2); err != nil {
		return nil, fmt.Errorf("failed to configure %s: %w", tap2Name, err)
	}

	// Create a bridge to connect the two interfaces
	bridgeName := "n2nbridge"
	if err := createBridge(bridgeName, []string{tap1Name, tap2Name}); err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}
	defer cleanupBridge(bridgeName)

	// Prepare buffers
	sendBuf := make([]byte, opts.PacketSize)
	recvBuf := make([]byte, 2048) // Larger for ethernet headers

	// Create ethernet frame
	for i := 0; i < 6; i++ {
		sendBuf[i] = 0xFF // Broadcast destination MAC
	}

	// Get MAC address of TAP1 for source
	mac1 := tap1.HardwareAddr()
	copy(sendBuf[6:12], mac1)

	// Set ethertype to IPv4
	sendBuf[12] = 0x08
	sendBuf[13] = 0x00

	// Setup IPv4 header
	// ...basic header for ping-like functionality

	// Measure using fixed ping-like packets
	var latencies []time.Duration

	// Start goroutine to listen on tap2
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for i := 0; i < opts.Iterations; i++ {
			n, err := tap2.Read(recvBuf)
			if err != nil {
				log.Printf("Error reading from TAP2: %v", err)
				continue
			}

			// Swap source and destination MACs
			for j := 0; j < 6; j++ {
				recvBuf[j], recvBuf[j+6] = recvBuf[j+6], recvBuf[j]
			}

			// Send response
			_, err = tap2.Write(recvBuf[:n])
			if err != nil {
				log.Printf("Error writing to TAP2: %v", err)
			}
		}
	}()

	// Send packets and measure RTT
	startTime := time.Now()

	for i := 0; i < opts.Iterations; i++ {
		sendTime := time.Now()

		// Send packet
		_, err = tap1.Write(sendBuf)
		if err != nil {
			log.Printf("Error writing to TAP1: %v", err)
			continue
		}

		// Receive response
		_, err = tap1.Read(recvBuf)
		if err != nil {
			log.Printf("Error reading from TAP1: %v", err)
			continue
		}

		// Calculate RTT
		rtt := time.Since(sendTime)
		latencies = append(latencies, rtt)
	}

	totalTime := time.Since(startTime)

	// Wait for the reader to finish
	wg.Wait()

	// Calculate statistics
	results := calculateStats(latencies, opts.Iterations, totalTime)
	results.PacketSize = opts.PacketSize
	results.Component = opts.Component

	return results, nil
}

// createBridge creates a network bridge and adds interfaces to it
func createBridge(bridgeName string, interfaces []string) error {
	// Create bridge
	cmd := exec.Command("ip", "link", "add", "name", bridgeName, "type", "bridge")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create bridge: %w", err)
	}

	// Add interfaces to bridge
	for _, iface := range interfaces {
		cmd = exec.Command("ip", "link", "set", "dev", iface, "master", bridgeName)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to add %s to bridge: %w", iface, err)
		}
	}

	// Bring up bridge
	cmd = exec.Command("ip", "link", "set", "dev", bridgeName, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring up bridge: %w", err)
	}

	return nil
}

// cleanupBridge removes a network bridge
func cleanupBridge(bridgeName string) error {
	cmd := exec.Command("ip", "link", "delete", bridgeName)
	return cmd.Run()
}

// benchmarkProtocolOnly measures protocol serialization/deserialization performance
func benchmarkProtocolOnly(opts *BenchmarkOptions) (*LatencyResults, error) {
	// Setup
	var latencies []time.Duration

	// Use provided packet size for test data
	testData := make([]byte, opts.PacketSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	header, _ := protocol.NewProtoVHeader(protocol.VersionV, 64, spec.TypeData, 1234, "testcommunity", nil, nil)

	// Get buffer pools
	headerBuf := buffers.HeaderBufferPool.Get()
	defer buffers.HeaderBufferPool.Put(headerBuf)

	// Create payload buffer - header + test data
	packetBuf := make([]byte, protocol.ProtoVHeaderSize+opts.PacketSize)

	startTime := time.Now()

	for i := 0; i < opts.Iterations; i++ {
		iterStart := time.Now()

		packetBuf = protocol.PackProtoVDatagram(header, testData)

		newHeader, payload, err := protocol.UnpackProtoVDatagram(packetBuf)
		if err != nil {
			log.Printf("Error unpacking datagram: %v", err)
			continue
		}

		// Simple verification to make sure the compiler doesn't optimize away
		if len(payload) != opts.PacketSize || newHeader.Version != header.Version {
			log.Printf("Verification failed!")
		}

		// Calculate latency
		iterEnd := time.Now()
		latency := iterEnd.Sub(iterStart)
		latencies = append(latencies, latency)
	}

	totalDuration := time.Since(startTime)

	// Calculate statistics
	results := calculateStats(latencies, opts.Iterations, totalDuration)
	results.PacketSize = opts.PacketSize
	results.Component = opts.Component

	return results, nil
}

// calculateStats calculates statistics from latency measurements
func calculateStats(latencies []time.Duration, iterations int, totalTime time.Duration) *LatencyResults {
	if len(latencies) == 0 {
		return &LatencyResults{
			PacketsSent: iterations,
			PacketsRecv: 0,
			TotalTime:   totalTime,
		}
	}

	// Sort latencies
	sortDurations(latencies)

	// Calculate statistics
	var sum time.Duration
	min := latencies[0]
	max := latencies[0]

	for _, latency := range latencies {
		sum += latency

		if latency < min {
			min = latency
		}
		if latency > max {
			max = latency
		}
	}

	avg := sum / time.Duration(len(latencies))
	median := latencies[len(latencies)/2]

	// Calculate percentiles
	p95Index := (len(latencies) * 95) / 100
	p99Index := (len(latencies) * 99) / 100

	p95 := latencies[p95Index]
	p99 := latencies[p99Index]

	return &LatencyResults{
		MinLatency:    min,
		MaxLatency:    max,
		AvgLatency:    avg,
		MedianLatency: median,
		P95Latency:    p95,
		P99Latency:    p99,
		PacketsSent:   iterations,
		PacketsRecv:   len(latencies),
		TotalTime:     totalTime,
	}
}

// sortDurations sorts a slice of durations
func sortDurations(durations []time.Duration) {
	for i := 0; i < len(durations); i++ {
		for j := i + 1; j < len(durations); j++ {
			if durations[i] > durations[j] {
				durations[i], durations[j] = durations[j], durations[i]
			}
		}
	}
}

// RunAllBenchmarks runs benchmarks for all components with the given options
func RunAllBenchmarks(baseOpts *BenchmarkOptions) ([]*LatencyResults, error) {
	var results []*LatencyResults

	components := []Component{
		ComponentProtocolOnly,
		ComponentUDPOnly,
		ComponentTAPOnly,
		ComponentAll,
	}

	for _, component := range components {
		opts := *baseOpts // Copy options
		opts.Component = component

		log.Printf("Running benchmark for %s...", component)
		result, err := BenchmarkLatency(&opts)
		if err != nil {
			log.Printf("Error benchmarking %s: %v", component, err)
			continue
		}

		results = append(results, result)
		PrintResults(component, result)
	}

	return results, nil
}

// PrintResults prints the results of a latency benchmark
func PrintResults(component Component, results *LatencyResults) {
	fmt.Printf("=== Latency Benchmark: %s ===\n", component)
	fmt.Printf("Packet Size: %d bytes\n", results.PacketSize)
	fmt.Printf("Packets Sent: %d\n", results.PacketsSent)
	fmt.Printf("Packets Received: %d\n", results.PacketsRecv)

	if results.PacketsSent > 0 {
		lossPercent := 100.0 - (float64(results.PacketsRecv)/float64(results.PacketsSent))*100.0
		fmt.Printf("Packet Loss: %.2f%%\n", lossPercent)
	}

	fmt.Printf("Total Time: %v\n", results.TotalTime)
	fmt.Printf("Min Latency: %v\n", results.MinLatency)
	fmt.Printf("Avg Latency: %v\n", results.AvgLatency)
	fmt.Printf("Median Latency: %v\n", results.MedianLatency)
	fmt.Printf("95th Percentile: %v\n", results.P95Latency)
	fmt.Printf("99th Percentile: %v\n", results.P99Latency)
	fmt.Printf("Max Latency: %v\n", results.MaxLatency)
	fmt.Println("==========================================")
}

// SaveResultsToFile saves benchmark results to a CSV file
func SaveResultsToFile(results []*LatencyResults, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write header
	f.WriteString("Component,PacketSize,PacketsSent,PacketsReceived,MinLatency,AvgLatency,MedianLatency,P95Latency,P99Latency,MaxLatency,TotalTime\n")

	// Write data
	for _, r := range results {
		f.WriteString(fmt.Sprintf("%s,%d,%d,%d,%v,%v,%v,%v,%v,%v,%v\n",
			r.Component,
			r.PacketSize,
			r.PacketsSent,
			r.PacketsRecv,
			r.MinLatency.Nanoseconds(),
			r.AvgLatency.Nanoseconds(),
			r.MedianLatency.Nanoseconds(),
			r.P95Latency.Nanoseconds(),
			r.P99Latency.Nanoseconds(),
			r.MaxLatency.Nanoseconds(),
			r.TotalTime.Nanoseconds()))
	}

	return nil
}

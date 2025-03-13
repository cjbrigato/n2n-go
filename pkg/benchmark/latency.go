package benchmark

import (
	"fmt"
	"log"
	"n2n-go/pkg/buffers"
	"n2n-go/pkg/protocol"
	"net"
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

// BenchmarkLatency measures latency for a specific component
func BenchmarkLatency(component Component, iterations int) (*LatencyResults, error) {
	switch component {
	case ComponentAll:
		return benchmarkEndToEnd(iterations)
	case ComponentUDPOnly:
		return benchmarkUDPOnly(iterations)
	case ComponentTAPOnly:
		return benchmarkTAPOnly(iterations)
	case ComponentProtocolOnly:
		return benchmarkProtocolOnly(iterations)
	default:
		return nil, fmt.Errorf("unknown component: %d", component)
	}
}

// benchmarkEndToEnd measures complete end-to-end latency
func benchmarkEndToEnd(iterations int) (*LatencyResults, error) {
	// Implementation specific to your architecture
	// This would involve setting up actual edge nodes and measuring ping times
	return nil, fmt.Errorf("not implemented")
}

// benchmarkUDPOnly measures pure UDP socket performance
func benchmarkUDPOnly(iterations int) (*LatencyResults, error) {
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
	deadline := time.Now().Add(5 * time.Second)
	conn1.SetReadDeadline(deadline)
	conn2.SetReadDeadline(deadline)

	// Prepare buffers
	sendBuf := make([]byte, 1000)
	recvBuf := make([]byte, 1000)

	var wg sync.WaitGroup
	var latencies []time.Duration

	// Start receiver goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
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

	for i := 0; i < iterations; i++ {
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
		latencies = append(latencies, rtt)
	}

	totalTime := time.Since(startTime)

	// Wait for receiver to finish
	wg.Wait()

	// Calculate statistics
	results := calculateStats(latencies, iterations, totalTime)

	return results, nil
}

// benchmarkTAPOnly measures TAP device read/write performance
func benchmarkTAPOnly(iterations int) (*LatencyResults, error) {
	// This would require setting up a TAP device and measuring read/write times
	return nil, fmt.Errorf("not implemented - requires TAP device setup")
}

// benchmarkProtocolOnly measures protocol serialization/deserialization performance
func benchmarkProtocolOnly(iterations int) (*LatencyResults, error) {
	// Setup
	var latencies []time.Duration
	header := protocol.NewHeader(3, 64, protocol.TypeData, 1234, "testcommunity", "source", "dest")

	buf := buffers.HeaderBufferPool.Get()
	defer buffers.HeaderBufferPool.Put(buf)

	totalTime := time.Now()

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// Marshal
		err := header.MarshalBinaryTo(buf)
		if err != nil {
			log.Printf("Error marshaling: %v", err)
			continue
		}

		// Unmarshal
		var newHeader protocol.Header
		err = newHeader.UnmarshalBinary(buf[:protocol.TotalHeaderSize])
		if err != nil {
			log.Printf("Error unmarshaling: %v", err)
			continue
		}

		// Calculate latency
		latency := time.Since(start)
		latencies = append(latencies, latency)
	}

	totalDuration := time.Since(totalTime)

	// Calculate statistics
	results := calculateStats(latencies, iterations, totalDuration)

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

// PrintResults prints the results of a latency benchmark
func PrintResults(component Component, results *LatencyResults) {
	fmt.Printf("=== Latency Benchmark: %s ===\n", component)
	fmt.Printf("Packets Sent: %d\n", results.PacketsSent)
	fmt.Printf("Packets Received: %d\n", results.PacketsRecv)
	fmt.Printf("Packet Loss: %.2f%%\n", 100.0-float64(results.PacketsRecv)/float64(results.PacketsSent)*100.0)
	fmt.Printf("Total Time: %v\n", results.TotalTime)
	fmt.Printf("Min Latency: %v\n", results.MinLatency)
	fmt.Printf("Avg Latency: %v\n", results.AvgLatency)
	fmt.Printf("Median Latency: %v\n", results.MedianLatency)
	fmt.Printf("95th Percentile: %v\n", results.P95Latency)
	fmt.Printf("99th Percentile: %v\n", results.P99Latency)
	fmt.Printf("Max Latency: %v\n", results.MaxLatency)
	fmt.Println("==========================================")
}

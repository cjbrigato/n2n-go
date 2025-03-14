package main

import (
	"flag"
	"fmt"
	"log"
	"n2n-go/pkg/benchmark"
	"os"
	"time"
)

// Command-line flags
var (
	tap1Flag       string
	tap2Flag       string
	ip1Flag        string
	ip2Flag        string
	sizeFlag       int
	iterationsFlag int
	useBridgeFlag  bool
	bridgeFlag     string
	debugFlag      bool
	timeoutFlag    int
	helpFlag       bool
)

func init() {
	flag.StringVar(&tap1Flag, "tap1", "n2ntest0", "Name for first TAP interface")
	flag.StringVar(&tap2Flag, "tap2", "n2ntest1", "Name for second TAP interface")
	flag.StringVar(&ip1Flag, "ip1", "10.0.0.1/24", "IP for first interface (CIDR notation)")
	flag.StringVar(&ip2Flag, "ip2", "10.0.0.2/24", "IP for second interface (CIDR notation)")
	flag.IntVar(&sizeFlag, "size", 1024, "Packet size in bytes")
	flag.IntVar(&iterationsFlag, "iterations", 1000, "Number of packets to send")
	flag.BoolVar(&useBridgeFlag, "bridge", true, "Whether to create a bridge between interfaces")
	flag.StringVar(&bridgeFlag, "bridgename", "n2nbridge", "Name of the bridge (if used)")
	flag.BoolVar(&debugFlag, "debug", false, "Enable debug output")
	flag.IntVar(&timeoutFlag, "timeout", 1000, "Timeout in milliseconds for packet responses")
	flag.BoolVar(&helpFlag, "help", false, "Show help")

	// Parse flags
	flag.Parse()

	// Show help if requested
	if helpFlag {
		printUsage()
		os.Exit(0)
	}
}

func printUsage() {
	fmt.Println("TAP Interface Benchmark Tool")
	fmt.Println("\nUsage: taptest [options]")
	fmt.Println("\nOptions:")
	flag.PrintDefaults()

	fmt.Println("\nExamples:")
	fmt.Println("  taptest --iterations 5000 --size 512")
	fmt.Println("  taptest --tap1 test0 --tap2 test1 --bridge=false")
	fmt.Println("  taptest --ip1 192.168.100.1/24 --ip2 192.168.100.2/24")
	fmt.Println("  taptest --debug --timeout 2000")
}

func main() {
	fmt.Println("TAP Interface Benchmark Tool")

	// Check if running as root
	if os.Geteuid() != 0 {
		log.Fatal("This program requires root privileges to create and configure TAP interfaces")
	}

	// Set debug flag in benchmark package if enabled
	if debugFlag {
		benchmark.DEBUG = true
	}

	// Configure benchmark options
	opts := &benchmark.TAPBenchmarkOptions{
		TAPInterface1: tap1Flag,
		TAPInterface2: tap2Flag,
		IP1:           ip1Flag,
		IP2:           ip2Flag,
		PacketSize:    sizeFlag,
		Iterations:    iterationsFlag,
		UseBridge:     useBridgeFlag,
		BridgeName:    bridgeFlag,
		Timeout:       time.Duration(timeoutFlag) * time.Millisecond,
		MaxDuration:   5 * time.Minute,
	}

	fmt.Println("\nBenchmark Configuration:")
	fmt.Printf("- TAP interfaces: %s and %s\n", opts.TAPInterface1, opts.TAPInterface2)
	fmt.Printf("- IP addresses: %s and %s\n", opts.IP1, opts.IP2)
	fmt.Printf("- Packet size: %d bytes\n", opts.PacketSize)
	fmt.Printf("- Iterations: %d\n", opts.Iterations)
	fmt.Printf("- Using bridge: %v\n", opts.UseBridge)
	if opts.UseBridge {
		fmt.Printf("- Bridge name: %s\n", opts.BridgeName)
	}
	fmt.Printf("- Timeout: %v\n", opts.Timeout)
	fmt.Printf("- Debug mode: %v\n", debugFlag)
	fmt.Println("\nStarting benchmark...")

	// Run the benchmark
	startTime := time.Now()
	results, err := benchmark.BenchmarkTAP(opts)
	if err != nil {
		log.Fatalf("Benchmark failed: %v", err)
	}

	fmt.Printf("\nBenchmark completed in %v\n", time.Since(startTime))

	// Print results
	fmt.Println("\nResults:")
	fmt.Printf("- Packets sent: %d\n", results.PacketsSent)
	fmt.Printf("- Packets received: %d\n", results.PacketsRecv)

	// Calculate packet loss
	var lossPercent float64
	if results.PacketsSent > 0 {
		lossPercent = 100.0 - (float64(results.PacketsRecv)/float64(results.PacketsSent))*100.0
		fmt.Printf("- Packet loss: %.2f%%\n", lossPercent)
	}

	// Only show latency stats if we actually received packets
	if results.PacketsRecv > 0 {
		fmt.Printf("- Min latency: %.3f µs\n", float64(results.MinLatency.Nanoseconds())/1000.0)
		fmt.Printf("- Avg latency: %.3f µs\n", float64(results.AvgLatency.Nanoseconds())/1000.0)
		fmt.Printf("- Median latency: %.3f µs\n", float64(results.MedianLatency.Nanoseconds())/1000.0)
		fmt.Printf("- 95th percentile: %.3f µs\n", float64(results.P95Latency.Nanoseconds())/1000.0)
		fmt.Printf("- 99th percentile: %.3f µs\n", float64(results.P99Latency.Nanoseconds())/1000.0)
		fmt.Printf("- Max latency: %.3f µs\n", float64(results.MaxLatency.Nanoseconds())/1000.0)
	} else {
		fmt.Println("- No latency statistics available - no packets were received")
	}

	// Performance assessment based on latency and packet loss
	fmt.Println("\nPerformance Assessment:")
	if results.PacketsRecv == 0 {
		fmt.Println("CRITICAL: No packets were received! This indicates a serious problem with the TAP interfaces or bridge configuration.")
		fmt.Println("Troubleshooting steps:")
		fmt.Println("1. Verify that both TAP interfaces were created successfully")
		fmt.Println("2. Check that the bridge is properly connecting the interfaces")
		fmt.Println("3. Ensure IP addresses are correctly configured")
		fmt.Println("4. Try running with --debug flag for more detailed logging")
		fmt.Println("5. Verify that you have sufficient permissions")
	} else if lossPercent > 50.0 {
		fmt.Printf("POOR: High packet loss (%.2f%%)! Your TAP implementation is dropping most packets.\n", lossPercent)
		avgLatencyUs := float64(results.AvgLatency.Nanoseconds()) / 1000.0
		fmt.Printf("Latency for received packets: %.2f µs\n", avgLatencyUs)
	} else if lossPercent > 10.0 {
		fmt.Printf("FAIR: Moderate packet loss (%.2f%%) with average latency of %.2f µs.\n",
			lossPercent, float64(results.AvgLatency.Nanoseconds())/1000.0)
		fmt.Println("Your TAP implementation is working but could be more reliable.")
	} else {
		avgLatencyUs := float64(results.AvgLatency.Nanoseconds()) / 1000.0
		if avgLatencyUs < 50.0 {
			fmt.Printf("EXCELLENT: Low packet loss (%.2f%%) and very low latency (%.2f µs).\n",
				lossPercent, avgLatencyUs)
		} else if avgLatencyUs < 100.0 {
			fmt.Printf("GOOD: Low packet loss (%.2f%%) and good latency (%.2f µs).\n",
				lossPercent, avgLatencyUs)
		} else if avgLatencyUs < 200.0 {
			fmt.Printf("AVERAGE: Low packet loss (%.2f%%) but moderate latency (%.2f µs).\n",
				lossPercent, avgLatencyUs)
		} else {
			fmt.Printf("BELOW AVERAGE: Low packet loss (%.2f%%) but high latency (%.2f µs).\n",
				lossPercent, avgLatencyUs)
		}
	}
}

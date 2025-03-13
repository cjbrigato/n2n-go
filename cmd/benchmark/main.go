package main

import (
	"flag"
	"fmt"
	"log"
	"n2n-go/pkg/benchmark"
	"os"
	"strings"
	"time"
)

// Version information - will be set at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Command-line flags
var (
	componentFlag     string
	iterationsFlag    int
	packetSizeFlag    int
	supernodeFlag     string
	communityFlag     string
	outputFlag        string
	allComponentsFlag bool
	tapIDFlag         int
	helpFlag          bool
)

func init() {
	flag.StringVar(&componentFlag, "component", "udp", "Component to benchmark (protocol, udp, tap, all)")
	flag.IntVar(&iterationsFlag, "iterations", 1000, "Number of iterations to run")
	flag.IntVar(&packetSizeFlag, "packetsize", 1024, "Packet size in bytes")
	flag.StringVar(&supernodeFlag, "supernode", "127.0.0.1:7777", "Supernode address for 'all' benchmark")
	flag.StringVar(&communityFlag, "community", "benchcommunity", "Community name for 'all' benchmark")
	flag.StringVar(&outputFlag, "output", "", "Output file for results (CSV format)")
	flag.BoolVar(&allComponentsFlag, "allcomponents", false, "Run benchmarks for all components")
	flag.IntVar(&tapIDFlag, "tapid", 10, "Base ID for TAP interfaces (will use tapid and tapid+1)")
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
	fmt.Printf("n2n-go Benchmark Tool %s (built %s)\n\n", Version, BuildTime)
	fmt.Println("Usage: benchmark [options]")
	fmt.Println("\nOptions:")
	flag.PrintDefaults()

	fmt.Println("\nExamples:")
	fmt.Println("  benchmark --component protocol --iterations 10000")
	fmt.Println("  benchmark --component udp --packetsize 512")
	fmt.Println("  benchmark --component all --supernode 192.168.1.100:7777")
	fmt.Println("  benchmark --allcomponents --output results.csv")
}

func parseComponent(compStr string) (benchmark.Component, error) {
	switch strings.ToLower(compStr) {
	case "protocol":
		return benchmark.ComponentProtocolOnly, nil
	case "udp":
		return benchmark.ComponentUDPOnly, nil
	case "tap":
		return benchmark.ComponentTAPOnly, nil
	case "all":
		return benchmark.ComponentAll, nil
	default:
		return 0, fmt.Errorf("unknown component: %s", compStr)
	}
}

func main() {
	fmt.Printf("n2n-go Benchmark Tool %s (built %s)\n\n", Version, BuildTime)

	// Check if user permissions are appropriate for certain benchmarks
	if os.Geteuid() != 0 && (componentFlag == "tap" || componentFlag == "all" || allComponentsFlag) {
		log.Println("WARNING: TAP benchmarks require root privileges. Please run as root or with sudo.")
		if !allComponentsFlag {
			log.Fatal("TAP benchmark requires root privileges. Exiting.")
		}
	}

	// Setup benchmark options
	opts := &benchmark.BenchmarkOptions{
		Iterations:     iterationsFlag,
		PacketSize:     packetSizeFlag,
		SupernodeAddr:  supernodeFlag,
		Community:      communityFlag,
		TAPInterfaceID: tapIDFlag,
	}

	// Store results
	var results []*benchmark.LatencyResults

	// Run benchmarks for all components or just the specified one
	if allComponentsFlag {
		log.Println("Running benchmarks for all components...")

		allResults, err := benchmark.RunAllBenchmarks(opts)
		if err != nil {
			log.Printf("Some benchmarks failed: %v", err)
		}
		results = append(results, allResults...)

	} else {
		// Parse component
		component, err := parseComponent(componentFlag)
		if err != nil {
			log.Fatalf("Invalid component: %v", err)
		}

		opts.Component = component

		// Run benchmark
		log.Printf("Running benchmark for %s...", component)
		log.Printf("Iterations: %d, Packet Size: %d bytes", opts.Iterations, opts.PacketSize)

		startTime := time.Now()
		result, err := benchmark.BenchmarkLatency(opts)
		if err != nil {
			log.Fatalf("Benchmark failed: %v", err)
		}

		log.Printf("Benchmark completed in %v", time.Since(startTime))

		// Print results
		benchmark.PrintResults(component, result)

		results = append(results, result)
	}

	// Save results to file if specified
	if outputFlag != "" && len(results) > 0 {
		log.Printf("Saving results to %s", outputFlag)
		if err := benchmark.SaveResultsToFile(results, outputFlag); err != nil {
			log.Fatalf("Failed to save results: %v", err)
		}
		log.Printf("Results saved successfully")
	}
}

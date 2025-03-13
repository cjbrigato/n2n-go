package main

import (
	"fmt"
	"n2n-go/pkg/benchmark"
)

func main() {
	// Benchmark protocol serialization/deserialization
	results, err := benchmark.BenchmarkLatency(benchmark.ComponentProtocolOnly, 10000)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	benchmark.PrintResults(benchmark.ComponentProtocolOnly, results)

	// Benchmark UDP performance
	results, err = benchmark.BenchmarkLatency(benchmark.ComponentUDPOnly, 1000)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	benchmark.PrintResults(benchmark.ComponentUDPOnly, results)
}

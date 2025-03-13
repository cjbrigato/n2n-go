#!/bin/bash
# n2n-go Benchmark Runner Script
# This script runs a comprehensive set of benchmarks for n2n-go

set -e

# Check if running as root/sudo
if [ "$EUID" -ne 0 ]; then
  echo "This script needs to run as root for TAP device creation."
  echo "Please run with sudo: sudo $0"
  exit 1
fi

# Create output directory
TIMESTAMP=$(date +"%Y%m%d-%H%M%S")
OUTPUT_DIR="benchmark_results_$TIMESTAMP"
mkdir -p "$OUTPUT_DIR"

echo "=========================================================="
echo "n2n-go Benchmark Suite"
echo "Results will be saved to: $OUTPUT_DIR"
echo "=========================================================="

# Build benchmark tool
echo "Building benchmark tool..."
(cd cmd/benchmark && go build)

# Run protocol benchmark with various packet sizes
echo "Running protocol benchmarks..."
for size in 64 256 512 1024 1500; do
  echo "Testing protocol with $size byte packets..."
  cmd/benchmark/benchmark --component protocol --iterations 10000 --packetsize $size --output "$OUTPUT_DIR/protocol_$size.csv"
done

# Run UDP benchmark with various packet sizes
echo "Running UDP benchmarks..."
for size in 64 256 512 1024 1500; do
  echo "Testing UDP with $size byte packets..."
  cmd/benchmark/benchmark --component udp --iterations 1000 --packetsize $size --output "$OUTPUT_DIR/udp_$size.csv"
done

# Run TAP benchmark with various packet sizes
echo "Running TAP benchmarks..."
for size in 64 256 512 1024 1500; do
  echo "Testing TAP with $size byte packets..."
  cmd/benchmark/benchmark --component tap --iterations 500 --packetsize $size --output "$OUTPUT_DIR/tap_$size.csv" --tapid $RANDOM
done

# Start a supernode for end-to-end testing
echo "Starting temporary supernode for end-to-end tests..."
(cd cmd/supernode && go build)
cmd/supernode/supernode --port 17777 &
SUPERNODE_PID=$!

# Make sure supernode is killed when script exits
trap "echo 'Stopping supernode...'; kill $SUPERNODE_PID 2>/dev/null || true" EXIT

# Wait for supernode to start
sleep 2

# Run end-to-end benchmark with various packet sizes
echo "Running end-to-end benchmarks..."
for size in 64 256 512 1024; do
  echo "Testing end-to-end with $size byte packets..."
  cmd/benchmark/benchmark --component all --iterations 100 --packetsize $size --supernode "127.0.0.1:17777" --output "$OUTPUT_DIR/all_$size.csv" --tapid $RANDOM
  
  # Clean up TAP interfaces between runs
  ip link show | grep n2ntest | awk '{print $2}' | tr -d ':' | xargs -I{} ip link delete {} 2>/dev/null || true
  sleep 1
done

# Run comparative benchmark (all components with same parameters)
echo "Running comparative benchmark..."
cmd/benchmark/benchmark --allcomponents --iterations 100 --packetsize 512 --output "$OUTPUT_DIR/comparative.csv" --tapid $RANDOM

echo "=========================================================="
echo "Benchmarks complete! Results saved to: $OUTPUT_DIR"
echo "=========================================================="

# Generate simple report
echo "# n2n-go Benchmark Results" > "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "Date: $(date)" >> "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "## Summary" >> "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "| Component | Packet Size | Avg Latency | Min Latency | P95 Latency |" >> "$OUTPUT_DIR/README.md"
echo "|-----------|-------------|-------------|-------------|-------------|" >> "$OUTPUT_DIR/README.md"

# Extract data from CSV files
for file in "$OUTPUT_DIR"/*.csv; do
  # Skip header line and process first data line only
  tail -n +2 "$file" | head -n 1 | while IFS=, read -r component size sent recv min avg median p95 p99 max total; do
    # Convert ns to μs
    min_us=$(echo "scale=3; $min/1000" | bc)
    avg_us=$(echo "scale=3; $avg/1000" | bc)
    p95_us=$(echo "scale=3; $p95/1000" | bc)
    
    echo "| $component | $size | ${avg_us}μs | ${min_us}μs | ${p95_us}μs |" >> "$OUTPUT_DIR/README.md"
  done
done

echo "" >> "$OUTPUT_DIR/README.md"
echo "## Recommendations" >> "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "Based on these results, here are the components that contribute most to latency:" >> "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "1. TBD (analyze results)" >> "$OUTPUT_DIR/README.md"
echo "2. TBD (analyze results)" >> "$OUTPUT_DIR/README.md"
echo "3. TBD (analyze results)" >> "$OUTPUT_DIR/README.md"
echo "" >> "$OUTPUT_DIR/README.md"
echo "See the CSV files in this directory for detailed measurements." >> "$OUTPUT_DIR/README.md"

echo "Summary report generated: $OUTPUT_DIR/README.md"

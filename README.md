# n2n-go

A high-performance layer 2 peer-to-peer VPN implementation in Go.

![n2n-go Network](https://via.placeholder.com/800x400?text=n2n-go+Network+Visualization)

## Overview

n2n-go is a robust Go implementation of the [n2n](https://github.com/ntop/n2n) peer-to-peer virtual private network. It allows you to create secure, encrypted virtual networks between computers, even behind NAT or firewalls, without requiring a central VPN server.

The network architecture follows a peer-to-peer model with two main components:

- **Supernode**: Acts as a meeting point for edges to discover each other and relay traffic when direct connections aren't possible
- **Edge**: Runs on each peer and handles the encrypted tunneling of traffic

Once edges discover each other through the supernode, they establish direct peer-to-peer connections when possible, minimizing latency and maximizing throughput.

## Key Features

- **Peer-to-Peer Communication**: Direct connections between edges when possible
- **Supernode Relay**: Fallback communication through supernode when direct connection isn't possible
- **Layer 2 Tunneling**: Uses TAP interfaces for true layer 2 virtual networks
- **Community Isolation**: Multiple isolated virtual networks using different community names
- **Encryption**: Optional AES-GCM encryption of all data packets
- **UPnP Support**: Automatic port forwarding on compatible routers
- **NAT Traversal**: Advanced techniques to establish direct connections behind NAT
- **Performance Benchmarking**: Comprehensive tools to test performance between nodes
- **Web UI**: Visualization of network topology and peer connections
- **Predictable MAC Addresses**: Generated based on machine ID and community
- **VFuze Data FastPath**: Optimized data path for reduced overhead and improved performance
- **Stable Virtual IPs**: IP address allocation with persistence

## OS Compatibility
- **Supernode**: can run on any GOOS
- **Edge**: only run on linux right now (compilation will fail early thanks to `github.com/cjbrigato/ensure` package) but windows/darwin/*bsd compatibility is planned

## Installation

### Prerequisites

- Go 1.16+
- Linux with TAP interface support
- Root/sudo privileges (required for creating TAP interfaces)

### Building from Source

```bash
git clone https://github.com/yourusername/n2n-go.git
cd n2n-go

# Build all components
go build ./...

# Build specific components
go build -o supernode ./cmd/supernode
go build -o edge ./cmd/edge
go build -o benchmark ./cmd/benchmark
```

## Quick Start

### Running a Supernode

```bash
sudo ./supernode
```

By default, the supernode listens on UDP port 7777. Configuration is read from `supernode.yaml`.

### Running an Edge

```bash
sudo ./edge -community acme -supernode 192.168.1.253:7777
```

This connects to a supernode at 192.168.1.253:7777 and joins the "acme" community.

## Configuration

Both the edge and supernode components can be configured with YAML files, environment variables, or command-line flags.

### Edge Configuration (edge.yaml)

```yaml
# edge.yaml
edge_id: ""  # auto generate from hostname
community: "acme"
supernode_addr: "157.180.44.113:7777"
local_port: 0  # Use 0 for automatic port assignment
enable_vfuze: true
tap_name: "n2n_tap0"
heartbeat_interval: "10s"
udp_buffer_size: 8388608
api_listen_address: ":7778"  # Optional API address
encryption_passphrase: "YourSecretPassphrase"  # Optional encryption
```

#### Edge Command-line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | `edge.yaml` |
| `-id` | Edge identifier | hostname |
| `-community` | Community name | - |
| `-tap` | TAP interface name | `n2n_tap0` |
| `-port` | Local UDP port | `0` (system-assigned) |
| `-enableFuze` | Enable VFuze fastpath | `true` |
| `-supernode` | Supernode address | - |
| `-heartbeat` | Heartbeat interval | `30s` |
| `-udpbuffersize` | UDP buffer size | `8388608` |
| `-api-listen` | API listen address | `:7778` |
| `-encryption-passphrase` | Passphrase for encryption | - |

### Supernode Configuration (supernode.yaml)

```yaml
# supernode.yaml
listen_address: ":7777"
debug: false
community_subnet: "10.128.0.0"
community_subnet_cidr: 24
expiry_duration: "90s"
cleanup_interval: "30s"
udp_buffer_size: 2097152
strict_hash_checking: true
enable_vfuze: true
```

#### Supernode Command-line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | `supernode.yaml` |
| `-listen` | UDP listen address | `:7777` |
| `-cleanup` | Cleanup interval for stale edges | `5m` |
| `-expiry` | Edge expiry duration | `10m` |
| `-debug` | Enable debug logging | `false` |
| `-enableFuze` | Enable VFuze fastpath | `true` |
| `-subnet` | Base subnet for communities | `10.128.0.0` |
| `-subnetcidr` | CIDR prefix length for community subnets | `24` |

## Architecture

n2n-go implements a peer-to-peer virtual network. 
You can see the network updating in realtime via the edge client api:

![api_peers_visualisation](https://github.com/user-attachments/assets/027586df-690f-4360-a413-150f32cce7c6)

1. Edges register with a Supernode
2. Supernode assists with peer discovery
3. Edges attempt to establish direct P2P connections
4. If direct connection fails, traffic is relayed through the Supernode
5. Layer 2 traffic is encapsulated in UDP packets

## Package Documentation

### pkg/benchmark

Provides comprehensive benchmark tools for measuring network performance.

**Key Components**:
- `BenchmarkLatency`: Measures latency between endpoints
- `BenchmarkTAP`: Tests TAP device performance
- Component benchmarks: Protocol, UDP, TAP, End-to-end
- Results analysis and statistics generation

**Usage Example**:
```go
opts := benchmark.DefaultBenchmarkOptions()
opts.Component = benchmark.ComponentUDPOnly
opts.Iterations = 5000
results, err := benchmark.BenchmarkLatency(opts)
benchmark.PrintResults(opts.Component, results)
```

### pkg/buffers

Provides buffer pool management to reduce memory allocations and GC pressure.

**Key Components**:
- `BufferPool`: A sync.Pool-based buffer recycling system
- Predefined pools for common packet sizes
- Thread-safe buffer acquisition and return

**Usage Example**:
```go
// Get a buffer from the pool
buf := buffers.PacketBufferPool.Get()
defer buffers.PacketBufferPool.Put(buf)

// Use the buffer
conn.ReadFromUDP(buf)
```

### pkg/crypto

Handles cryptographic operations for secure communication.

**Key Components**:
- AES-GCM encryption/decryption for data packets
- RSA key pair generation for supernode authentication
- Key derivation from passphrases
- Sequence encryption for machine ID protection

**Usage Example**:
```go
// Generate encryption key from passphrase
key := crypto.KeyFromPassphrase("mySecretPassphrase")

// Encrypt payload
encrypted, err := crypto.EncryptPayload(key, plaintext)

// Decrypt payload
decrypted, err := crypto.DecryptPayload(key, encrypted)
```

### pkg/edge

Implements the edge client that connects to the network.

**Key Components**:
- `EdgeClient`: Main client implementation
- Configuration management
- TAP interface handling
- Peer-to-peer communication
- UPnP port forwarding
- API for web visualization
- Registration and heartbeat protocols

**Usage Example**:
```go
cfg := edge.DefaultConfig()
cfg.Community = "mycommunity"
cfg.SupernodeAddr = "supernode.example.com:7777"

client, err := edge.NewEdgeClient(cfg)
if err != nil {
    log.Fatalf("Failed to create edge client: %v", err)
}

if err := client.Setup(); err != nil {
    log.Fatalf("Failed to setup: %v", err)
}

client.Run() // Starts the edge client
```

### pkg/machine

Handles machine identification and MAC address generation.

**Key Components**:
- Machine ID generation and storage
- Deterministic MAC address generation
- Community-specific device identifiers

**Usage Example**:
```go
// Get machine ID
machineID, err := machine.GetMachineID()

// Generate MAC address for a community
mac, err := machine.GenerateMac("mycommunity")
```

### pkg/p2p

Manages peer-to-peer communication and state tracking.

**Key Components**:
- `PeerRegistry`: Tracks known peers and their status
- `P2PCapacity`: Defines peer connection quality types
- Network visualization with Graphviz
- Peer state synchronization
- Connection quality monitoring

**Usage Example**:
```go
registry := p2p.NewPeerRegistry()
peer, err := registry.AddPeer(peerInfo, false)
peers := registry.GetP2PAvailablePeers()

// Generate visualization
state, _ := p2p.NewCommunityP2PState("mycommunity", p2pState)
dotGraph := state.GenerateP2PGraphviz()
```

### pkg/protocol

Defines the network protocol used for communication.

**Key Components**:
- `ProtoVHeader`: Protocol header structure
- Protocol encoding/decoding
- Message handlers
- Hash verification
- Header optimization for reduced overhead
- VFuze protocol for fast data path

**Usage Example**:
```go
header, err := protocol.NewProtoVHeader(
    protocol.VersionV,
    64,                      // TTL
    spec.TypeData,           // Packet type
    seq,                     // Sequence number
    "mycommunity",           // Community name
    srcMAC,                  // Source MAC
    dstMAC,                  // Destination MAC
)

data, err := header.MarshalBinary()
```

### pkg/supernode

Implements the supernode server that coordinates the network.

**Key Components**:
- `Supernode`: Main server implementation
- `Community`: Community management and isolation
- Edge registration and tracking
- IP address allocation
- Packet forwarding and routing
- Peer discovery facilitation
- Stale edge cleanup

**Usage Example**:
```go
config := supernode.DefaultConfig()
config.ListenAddr = ":7777"

conn, _ := net.ListenUDP("udp", udpAddr)
sn := supernode.NewSupernodeWithConfig(conn, config)

sn.Listen() // Starts the supernode
```

### pkg/tuntap

Provides TAP interface management for the virtual network.

**Key Components**:
- `Interface`: TAP interface wrapper
- Ethernet frame parsing and construction
- MAC address handling
- Link configuration

**Usage Example**:
```go
// Create a new TAP interface
tap, err := tuntap.NewInterface("n2n0", "tap")
if err != nil {
    log.Fatalf("Failed to create TAP: %v", err)
}
defer tap.Close()

// Read/write Ethernet frames
frame := make([]byte, 2048)
n, err := tap.Read(frame)
```

### pkg/upnp

Handles UPnP protocol for automatic port forwarding.

**Key Components**:
- `UPnPClient`: UPnP client implementation
- IGD (Internet Gateway Device) discovery
- Port mapping creation and deletion
- External IP address discovery

**Usage Example**:
```go
client, err := upnp.NewUPnPClient()
if err != nil {
    log.Printf("UPnP not available: %v", err)
    return
}

// Add port mapping
err = client.AddPortMapping(
    "udp",           // Protocol
    7777,            // External port
    7777,            // Internal port
    "n2n-go client", // Description
    0,               // Lease duration (0 = indefinite)
)
```

### pkg/util

Provides utility functions used across the project.

**Key Components**:
- Gratuitous ARP packet generation
- Interface configuration
- Debug utilities
- Byte slice inspection

**Usage Example**:
```go
// Configure network interface
err := util.IfUp("n2n0", "10.0.0.1/24")

// Send gratuitous ARP announcement
err := util.SendGratuitousARP("n2n0", macAddr, net.ParseIP("10.0.0.1"))
```

## Command Line Tools

### cmd/edge

The edge client that connects to the n2n network.

```bash
sudo ./edge -community mycommunity -supernode node.example.com:7777
```

### cmd/supernode

The supernode server that facilitates edge discovery.

```bash
sudo ./supernode -listen :7777
```

### cmd/benchmark

A benchmarking tool for testing n2n-go performance.

```bash
sudo ./benchmark --component udp --iterations 10000 --packetsize 1024
sudo ./benchmark --component all --supernode 192.168.1.100:7777
sudo ./benchmark --allcomponents --output results.csv
```

### cmd/taptest

A tool to benchmark TAP interface performance.

```bash
sudo ./taptest --iterations 5000 --size 1024
```

### cmd/upnp

A utility for managing UPnP port forwarding.

```bash
./upnp --external-port 7777 --internal-port 7777 --protocol udp
```

### cmd/p2viz

A visualization tool for peer-to-peer network state.

```bash
./p2viz
```

## Network Visualization

n2n-go includes a web-based visualization of the network topology. When an edge is running with the API enabled, you can access:

- `http://localhost:7778/peers` - HTML network visualization
- `http://localhost:7778/peers.svg` - SVG network graph
- `http://localhost:7778/peers.dot` - Graphviz DOT file
- `http://localhost:7778/peers.json` - JSON data of peer connections
- `http://localhost:7778/leases.json` - IP address lease information

## Technical Details

### Protocol

n2n-go implements a compact, efficient, custom protocol:

- **Version**: Protocol version 5
- **Header Size**: Compact 30-byte header
- **VFuze FastPath**: Even more compact 7-byte header for data packets
- **Community Hash**: 32-bit hash for rapid community identification
- **Flags**: Support for protocol extensions and features
- **Timestamp**: For replay protection
- **Checksum**: For data integrity verification

### Packet Types

| Type | Description |
|------|-------------|
| TypeRegisterRequest | Edge registration with supernode |
| TypeUnregisterRequest | Edge unregistration |
| TypeHeartbeat | Keep-alive message |
| TypeData | Layer 2 traffic |
| TypePeerListRequest | Request for peer information |
| TypePeerInfo | Peer information sharing |
| TypePing | Connection testing and NAT traversal |
| TypeP2PStateInfo | Peer connection quality information |
| TypeP2PFullState | Complete network state |
| TypeLeasesInfos | IP address lease information |
| TypeSNPublicSecret | Supernode public key distribution |
| TypeRegisterResponse | Registration confirmation |

### Security Considerations

- **Community Isolation**: Different communities are isolated from each other
- **Encryption**: Optional AES-GCM encryption of all data
- **Machine ID**: Encrypted machine identity verification
- **RSA Authentication**: Public/private key verification between supernode and edges
- **Checksum Verification**: Ensures packet integrity

## Performance Tuning

### UDP Buffer Size

Increasing UDP buffer sizes can significantly improve performance:

```yaml
# Edge configuration
udp_buffer_size: 8388608  # 8MB buffer

# Supernode configuration  
udp_buffer_size: 2097152  # 2MB buffer
```

### VFuze FastPath

Enable VFuze for optimized data transfer with reduced overhead:

```yaml
enable_vfuze: true
```

### TAP Interface MTU

Set an appropriate MTU to avoid fragmentation:

```bash
ip link set dev n2n_tap0 mtu 1420
```

## Troubleshooting

### Common Issues

1. **TAP Interface Creation Fails**: 
   - Make sure you're running with sudo/root privileges
   - Check if the TUN/TAP kernel module is loaded: `modprobe tun`

2. **No Peer-to-Peer Connection**:
   - Check if UPnP is available on your router
   - Ensure both edges can reach the supernode
   - Try manually forwarding ports

3. **Connection Drops/Timeouts**:
   - Increase heartbeat interval for unstable connections
   - Check for restrictive firewalls
   - Ensure correct community name is used

### Diagnostic Commands

```bash
# Check TAP interface
ip addr show dev n2n_tap0

# Test connectivity
ping -I n2n_tap0 10.128.0.x

# Check open ports
sudo lsof -i :7777

# View network visualization
curl http://localhost:7778/peers.json
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Original n2n project by ntop
- The Go community for excellent networking packages

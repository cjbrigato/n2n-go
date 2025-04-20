# n2n-go

A high-performance layer 2 peer-to-peer VPN implementation in Go.

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
- **Edge**: can run on linux, windows (if you are dedicated and lucky), darwin, netbsd (experimental), *bsd compatibility is planned

## Installation

### Prerequisites

- Go 1.16+
- Linux with TAP interface support
- Windows with [tap-windows6](https://github.com/OpenVPN/tap-windows6) driver installed as "TAP0901" ComponentID
- Root/sudo privileges (required for creating TAP interfaces) / Administrator privileges on Windows (required for nearly everything...)

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
compress_payload: false # Optional zstd compression
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
| `-compress-payload` | Enable Zstd compression for data payloads | `false` |

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

### pkg/appdir

Provides utilities for determining and managing the application's configuration/data directory (`~/.n2n-go`).

**Key Components**:
- `AppDir()`: Returns the path to the application directory.
- Ensures the directory exists on initialization.

**Usage Example**:
```go
import "n2n-go/pkg/appdir"
// ...
logPath := path.Join(appdir.AppDir(), "edge.log")
```

### pkg/benchmark

Provides comprehensive benchmark tools for measuring network performance.

**Key Components**:
- `BenchmarkLatency`: Measures latency between endpoints for various components (UDP, TAP, Protocol, End-to-End).
- `BenchmarkTAP`: Tests TAP device performance specifically.
- Component benchmarks: Protocol, UDP, TAP, End-to-end.
- Results analysis and statistics generation.

**Usage Example**:
```go
opts := benchmark.DefaultBenchmarkOptions()
opts.Component = benchmark.ComponentUDPOnly
opts.Iterations = 5000
results, err := benchmark.BenchmarkLatency(opts)
benchmark.PrintResults(opts.Component, results)
```

### pkg/buffers

Provides buffer pool management using `sync.Pool` to reduce memory allocations and GC pressure for network packets and headers.

**Key Components**:
- `BufferPool`: A sync.Pool-based buffer recycling system.
- Predefined pools (`PacketBufferPool`, `HeaderBufferPool`) for common sizes.
- Thread-safe buffer acquisition (`Get()`) and return (`Put()`).

**Usage Example**:
```go
// Get a buffer from the pool
buf := buffers.PacketBufferPool.Get()
defer buffers.PacketBufferPool.Put(buf)

// Use the buffer
n, addr, err := conn.ReadFromUDP(buf)
```

### pkg/crypto

Handles cryptographic operations for secure communication and identity.

**Key Components**:
- AES-GCM encryption/decryption (`aesGCMTransform`).
- Key derivation from passphrases.
- RSA key pair generation and management for supernode authentication (`SNSecrets`).
- Encryption/decryption using RSA keys (`EncryptSequence`, `DecryptSequence`).
- PEM encoding/decoding for public keys.

**Usage Example**:
```go
// Generate encryption key from passphrase
aesTransform, err := transform.NewAESGCMTransform("mySecretPassphrase")

// Encrypt payload
encrypted, err := aesTransform.Apply(plaintext)

// Decrypt payload
decrypted, err := aesTransform.Reverse(encrypted)
```

### pkg/edge

Implements the edge client that connects to the n2n network.

**Key Components**:
- `EdgeClient`: Main client implementation.
- Configuration loading (`LoadConfig`, `Config`).
- TAP interface handling via `pkg/tuntap`.
- Peer-to-peer state management via `pkg/p2p`.
- Supernode registration, heartbeat, and communication.
- UDP packet handling (VFuze and standard protocol).
- Payload processing pipeline (`pkg/transform`).
- UPnP/NAT-PMP port forwarding via `pkg/natclient`.
- API server (`EdgeClientApi`) for web visualization and status.
- Message handlers for different packet types.

**Usage Example**:
```go
cfg, err := edge.LoadConfig(true) // Load config from file/env/flags
client, err := edge.NewEdgeClient(*cfg)
if err := client.InitialSetup(); err != nil { log.Fatalf("Setup failed: %v", err) }
client.Run() // Starts the edge client loops
```

### pkg/log

Provides a Zerolog-based logger that writes JSON logs to an SQLite database.

**Key Components**:
- `Init()`: Initializes the logger with a database file path.
- Standard logging functions (`Printf`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`, `Panic`).
- `GetLastNLogs()`, `GetLogsBetween()`, `GetLogsSince()`: Functions to retrieve log entries from the database.
- `Close()`: Closes the logger and the database connection.

**Usage Example**:
```go
log.MustInit("edge") // Initialize logger for the edge app
log.Printf("Edge client starting...")
log.Info().Str("community", cfg.Community).Msg("Joining community")
// ...
log.Close()
```

### pkg/machine

Handles machine identification and deterministic MAC address generation based on machine ID and community name.

**Key Components**:
- `GetMachineID()`: Retrieves or generates a persistent machine ID stored in the app directory.
- `GenerateMac()`: Generates a unique, predictable MAC address for a specific community using the machine ID.
- Hashing function (`notSipHash24`) used for MAC generation.

**Usage Example**:
```go
// Get machine ID
machineID, err := machine.GetMachineID()

// Generate MAC address for a community
mac, err := machine.GenerateMac("mycommunity")
```

### pkg/management

Provides a client and server for inter-process communication via Unix sockets (or named pipes on Windows) for controlling and querying the edge/supernode daemons.

**Key Components**:
- `ManagementServer`: Listens on a socket, handles commands.
- `ManagementClient`: Connects to the socket, sends commands.
- Command registration (`RegisterHandler`) and handling.
- Password authentication.
- Default commands: `status`, `ping`, `logs`, `help`.

**Usage Example**:
```go
// Server side (in edge/supernode)
mgmtServer := management.NewManagementServer("edge", cfg.Community) // Password is community name
mgmtServer.RegisterHandler("peers", "List connected peers", handlePeersCmd)
mgmtServer.Start()

// Client side (in a separate tool or CLI)
client := management.NewManagementClient("edge", "community_password")
response, err := client.SendCommand("peers")
fmt.Println(response)
```

### pkg/natclient

Handles NAT traversal using UPnP (IGDv1/IGDv2) and NAT-PMP/PCP protocols.

**Key Components**:
- `NATClient` interface: Defines common methods (`AddPortMapping`, `DeletePortMapping`, `GetExternalIP`, etc.).
- `SetupNAT()`: Attempts to discover and configure UPnP or NAT-PMP, returning a `NATClient`.
- `Cleanup()`: Removes mappings created by a client instance.
- Internal implementations for UPnP (`UPnPClient`) and NAT-PMP (`NATPMPClient`).

**Usage Example**:
```go
// In edge setup
natClient := natclient.SetupNAT(edge.Conn, edge.ID, edge.SupernodeAddr.String())
// ...
// On shutdown
natclient.Cleanup(natClient)
```

### pkg/p2p

Manages peer-to-peer state, connections, and network visualization.

**Key Components**:
- `PeerRegistry`: Tracks known peers (`Peer`), their addresses (`PeerInfo`), and P2P connection status (`P2PCapacity`).
- `PeerP2PInfos`, `P2PFullState`: Structures for exchanging P2P state information.
- `UDPWriteStrategy`: Defines how UDP packets are sent (Supernode relay, Best Effort P2P, Enforce P2P).
- Network visualization generation (Graphviz DOT format, HTML).

**Usage Example**:
```go
registry := p2p.NewPeerRegistry(communityName)
peerInfoList := receivePeerInfoFromSupernode()
registry.HandlePeerInfoList(peerInfoList, false, true) // Update registry
peers := registry.GetP2PAvailablePeers()
dotGraph := registry.GenPeersDot() // Generate visualization
```

### pkg/protocol

Defines the core n2n-go network protocol (version V and VFuze), including header structures, packet types, encoding/decoding, and message handling.

**Key Components**:
- `ProtoVHeader`: Standard 30-byte protocol header.
- `ProtoVFuzeHeader`: Compact 7-byte header for data fast path.
- `PacketType`: Enum defining different message types (Register, Data, Ping, etc.) (defined in `spec` sub-package).
- `PacketFlag`: Flags within the header for options (e.g., `FlagFromSuperNode`).
- `RawMessage`: Represents a received packet with header and payload.
- `Message[T]`: Generic wrapper for typed messages.
- `MessageHandlerMap`: For routing received packets to appropriate handlers.
- `codec`: Sub-package for generic Gob encoding/decoding.
- `netstruct`: Sub-package defining structures for specific message types.
- `spec`: Sub-package defining constants like `PacketType`.

**Usage Example**:
```go
// Create header
header := edge.EdgeHeader(spec.TypeHeartbeat, nil) // Create heartbeat header

// Pack datagram
packet := protocol.PackProtoVDatagram(header, payload)

// Unpack datagram
recvdHeader, recvdPayload, err := protocol.UnpackProtoVDatagram(packet)

// Handle typed message
rawMsg, _ := protocol.NewRawMessage(packet, addr)
err := edge.messageHandlers.Handle(rawMsg)
```

### pkg/supernode

Implements the supernode server that coordinates the network, manages communities, and facilitates peer discovery.

**Key Components**:
- `Supernode`: Main server implementation.
- `Community`: Manages edges within a specific community, including IP allocation (`ippool`).
- `Edge`: Represents a registered edge node.
- Configuration loading (`LoadConfig`, `Config`).
- UDP listener loop and packet processing (`ProcessPacket`).
- Message handlers for registration, heartbeats, peer requests, etc.
- Packet forwarding (unicast and broadcast) and relay logic.
- Stale edge cleanup mechanism.
- Network allocation for communities (`NetworkAllocator`).
- RSA key management for edge authentication (`SNSecrets`).

**Usage Example**:
```go
cfg, err := supernode.LoadConfig()
udpAddr, _ := net.ResolveUDPAddr("udp", cfg.ListenAddr)
conn, _ := net.ListenUDP("udp", udpAddr)
sn := supernode.NewSupernodeWithConfig(conn, cfg)
sn.Listen() // Starts the supernode listener loop
```

### pkg/syshosts

Provides utilities for reading, manipulating, and writing the system's hosts file (`/etc/hosts` or Windows equivalent).

**Key Components**:
- `Hosts`: Represents the hosts file content.
- `NewHosts()`: Loads the system hosts file.
- `Add()`, `Remove()`, `RemoveByHostname()`, `RemoveByIP()`: Modify entries in memory.
- `Has()`, `HasHostname()`, `HasIP()`: Check for existing entries.
- `Write()`: Saves changes back to the hosts file (requires permissions).
- `Reload()`: Reloads content from disk.
- `Clean()`, `SortHosts()`, `SortIPs()`: Utility functions for organizing entries.

**Usage Example**:
```go
hosts, err := syshosts.NewHosts()
if err != nil { log.Fatal(err) }
if !hosts.HasHostname("peer1.mycommunity") {
    hosts.Add("10.128.0.5", "peer1.mycommunity")
    if hosts.IsWritable() {
        err = hosts.Write()
        if err != nil { log.Printf("Failed to write hosts file: %v", err) }
    } else {
        log.Printf("Hosts file not writable.")
    }
}
```

### pkg/transform

Defines an interface and implementations for processing packet payloads, such as encryption or compression. Used by `pkg/edge`.

**Key Components**:
- `Transform` interface: `Apply` (for outgoing) and `Reverse` (for incoming) methods.
- `PayloadProcessor`: Manages a pipeline of `Transform` implementations.
- `aesGCMTransform`: Implements AES-GCM encryption/decryption.
- `zstdTransform`: Implements Zstandard compression/decompression.
- `noOpTransform`: Pass-through transform when no processing is needed.
- `gzipTransform`: Deprecated gzip implementation.

**Usage Example**:
```go
// In edge setup
transforms := []transform.Transform{}
if cfg.CompressPayload { transforms = append(transforms, transform.NewZstdTransform(...)) }
if cfg.EncryptionPassphrase != "" { transforms = append(transforms, transform.NewAESGCMTransform(...)) }
if len(transforms) == 0 { transforms = append(transforms, transform.NewNoOpTransform()) }
payloadProcessor, _ := transform.NewPayloadProcessor(transforms)

// Processing outgoing data
processedPayload, err := payloadProcessor.PrepareOutput(originalPayload)

// Processing incoming data
originalPayload, err := payloadProcessor.ParseInput(receivedPayload)
```

### pkg/tuntap

Provides a cross-platform abstraction for creating and managing TUN/TAP network interfaces.

**Key Components**:
- `Interface`: Wrapper around the platform-specific device.
- `Device`: Platform-specific implementation (Linux, Windows, Darwin).
- `Config`: Configuration for creating the interface.
- `NewInterface()`: Creates a new TAP interface based on config and OS.
- `Read()`, `Write()`: Read/write Ethernet frames.
- `ConfigureInterface()`, `IfUp()`, `IfMac()`: Platform-specific methods for setting IP, MAC, MTU, etc. (using netlink, ifconfig, or Windows APIs).
- `SendGratuitousARP()`: Sends GARP packets.
- Frame parsing utilities (`frame.go`).
- Windows overlapped I/O handling (`overlapped_windows.go`).

**Usage Example**:
```go
// Create a new TAP interface
tapCfg := tuntap.Config{Name: "n2n0", DevType: tuntap.TAP, MACAddress: mac.String()}
tap, err := tuntap.NewInterface(tapCfg)
if err != nil { log.Fatalf("Failed to create TAP: %v", err) }
defer tap.Close()

// Configure and bring up
err = tap.IfUp("10.128.0.5/24")

// Read/write Ethernet frames
frame := make([]byte, 2048)
n, err := tap.Read(frame)
_, err = tap.Write(outgoingFrame)
```

### pkg/util

Provides miscellaneous utility functions used across the project. (Currently minimal based on provided files).

**Key Components**:
- `DumpByteSlice()`: Debug utility to print byte slices in hex/ASCII format.
- *(Potentially other utilities like `IfUp`, `SendGratuitousARP` if they were moved here, but they seem to be in `tuntap` or `edge`)*

**Usage Example**:
```go
util.DumpByteSlice(packetData)
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
| TypeRetryRegisterRequest | Request edge to re-register (e.g., after SN restart) |
| TypeOnlineCheck | Check if an edge is online (future use) |
| TypeAck | Generic acknowledgement (future use) |


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
   - On Windows, ensure the TAP-Windows driver (ComponentID TAP0901) is installed correctly.

2. **No Peer-to-Peer Connection**:
   - Check if UPnP or NAT-PMP succeeded (check edge logs).
   - Ensure both edges can reach the supernode.
   - Try manually forwarding the UDP port used by the edge.
   - Firewalls on either peer or the network might be blocking direct UDP traffic.

3. **Connection Drops/Timeouts**:
   - Increase heartbeat interval (`heartbeat_interval` in edge config) for unstable connections.
   - Check for restrictive firewalls blocking UDP.
   - Ensure correct community name is used on all edges.
   - Verify supernode is reachable and stable.

4. **Authentication Errors**:
   - Ensure the supernode is running and accessible.
   - Check edge logs for specific RSA decryption/verification errors.
   - `RetryRegisterRequest` loops might indicate the supernode restarted and lost state, or machine ID issues.

### Diagnostic Commands

```bash
# Check TAP interface (Linux)
ip addr show dev n2n_tap0

# Check TAP interface (Windows)
ipconfig /all

# Test connectivity (replace with actual virtual IPs)
ping -I n2n_tap0 10.128.0.x # Linux
ping 10.128.0.x             # Windows

# Check open ports (Linux)
sudo ss -ulnp | grep 7777 # Check supernode port
sudo ss -ulnp | grep <edge_port> # Check edge port

# Check open ports (Windows)
netstat -ano | findstr "UDP" | findstr "<edge_port>"

# View network visualization/data (if edge API is enabled)
curl http://localhost:7778/peers.json
curl http://localhost:7778/leases.json

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

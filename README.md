# n2n-go

**n2n-go** is a complete rewrite of the popular n2n project in Go. It re-implements core functionality—including cryptographic transforms, edge and supernode management, protocol framing, virtual networking, and more—in an idiomatic, modular way.

## Repository Structure

```
n2n-go/
├── go.mod                  # Module file
├── cmd/
│   ├── edge/
│   │    └── main.go        # Edge client executable – sets up a TUN interface, registers with supernode, forwards traffic and sends heartbeats.
│   └── supernode/
│        └── main.go        # Supernode executable – listens on a UDP socket, maintains edge registry, processes registration/heartbeat messages.
├── pkg/
│   ├── aes/                # AES-CBC encryption with PKCS#7 padding.
│   ├── auth/               # Key generation, binary/ASCII conversion, and key exchange utilities.
│   ├── cc20/               # ChaCha20 (as a CC20 substitute) encryption functions.
│   ├── curve25519/         # Curve25519 key pair generation and shared secret computation.
│   ├── trafficfilter/      # Network traffic filtering (by IP/protocol).
│   ├── hexdump/            # Utility for generating hex dumps of binary data.
│   ├── json/               # Minimal JSON parser wrapping the standard library.
│   ├── minilzo/            # Pure-Go LZO1X compression and decompression.
│   ├── n2nregex/          # Regular expression utilities.
│   ├── pearson/            # Pearson hashing (8-bit and 64-bit).
│   ├── random/             # XORSHIFT128+ random number generator seeded from crypto/rand.
│   ├── portmap/            # In-memory port mapping (e.g., “80:8080”) management.
│   ├── management/         # Edge registration, virtual IP allocation, heartbeat management, and cleanup.
│   ├── supernode/          # Supernode-specific logic: managing edge records and processing UDP packets.
│   ├── edge/               # Edge-specific logic: TUN interface setup, UDP registration, heartbeat, and traffic forwarding.
│   ├── protocol/           # Packet header framing, checksum computation, and replay protection.
│   ├── util/               # Low-level utility functions and string buffering.
│   └── transform/          # Data transforms: null, AES, CC20, SPECK, TF (XOR), LZO, and Zstd.
└── README.md               # This file
```

## Features

- **Cryptographic Transforms:**  
  Provides multiple ciphers and transforms (AES, ChaCha20/CC20, SPECK, TF, LZO, Zstd) for securing data payloads and headers.

- **Protocol Framing:**  
  Implements packet header construction (version, TTL, sequence, timestamp, checksum, community) with checksum and timestamp verification for replay protection.

- **Edge and Supernode Management:**  
  Edge clients register with a supernode over UDP. Supernodes maintain a registry of edges, update heartbeats, and remove stale records.

- **Virtual Networking:**  
  Uses a TUN interface on the edge side for forwarding IP packets, allowing creation of a virtual network overlay.

- **Additional Utilities:**  
  Includes port mapping, random number generation, JSON parsing, regular expressions, and low-level helpers.

## Requirements

- **Go:** Version 1.20 or later is recommended.
- **Dependencies:** See the `go.mod` file for a complete list of imported packages.

## Build and Run

### Supernode

To build the supernode executable:

```bash
cd cmd/supernode
go build -o supernode .
```

Run the supernode (default listens on UDP port 7777):

```bash
./supernode -port=7777 -cleanup=5m -expiry=10m
```

### Edge

To build the edge client executable:

```bash
cd cmd/edge
go build -o edge .
```

Run the edge client (example):

```bash
./edge -id="edge01" -community="n2ncommunity" -tun="n2n0" -port=0 -supernode="supernode.example.com:7777" -heartbeat=30s
```

- Use `-id` for the unique edge identifier.
- Use `-community` for the network/community name.
- Use `-tun` to specify the TUN interface name.
- `-port` can be 0 for system-assigned local UDP port.
- `-supernode` is the address of the supernode (host:port).
- `-heartbeat` specifies the interval for heartbeat messages.

## Testing

Each package includes its own tests. To run tests for the entire project:

```bash
go test ./...
```

## Contributing

Contributions are welcome! Please feel free to fork this repository and submit pull requests.

## License

This project is licensed under the [GPL-3.0 License](https://www.gnu.org/licenses/gpl-3.0.html).

## Acknowledgments

This project is inspired by the original n2n C codebase and re-implemented in Go to provide a modern, modular, and idiomatic codebase.


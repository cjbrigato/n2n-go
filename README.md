# n2n-go

**n2n-go** is a complete rewrite of the popular n2n project in Go. It aims to re-implements core functionality—including cryptographic transforms, edge and supernode management, protocol framing, virtual networking, and more—in an idiomatic, modular way.

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
│   ├── pearson/            # Pearson hashing (8-bit and 64-bit).
│   ├── supernode/          # Supernode-specific logic: managing edge records and processing UDP packets.
│   ├── edge/               # Edge-specific logic: TUN interface setup, UDP registration, heartbeat, and traffic forwarding.
│   ├── tuntap/             # Tun/Tap interface abstraction (currently lacks win32 support)
│   └── protocol/           # Packet header framing, checksum computation, and replay protection.
└── README.md               # This file
```

## Features


- **Protocol Framing:**  
  Implements packet header construction (version, TTL, sequence, timestamp, checksum, community) with checksum and timestamp verification for replay protection.

- **Edge and Supernode Management:**  
  Edge clients register with a supernode over UDP. Supernodes maintain a registry of edges, update heartbeats, and remove stale records.

- **Virtual Networking:**  
  Uses a TAP interface on the edge side for forwarding IP packets, allowing creation of a virtual network overlay.

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
./edge -id="edge01" -community="n2ncommunity" -tap="n2n0" -port=0 -supernode="supernode.example.com:7777" -heartbeat=30s
```

- Use `-id` for the unique edge identifier.
- Use `-community` for the network/community name.
- Use `-tap` to specify the TAP interface name.
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


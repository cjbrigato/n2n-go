// Package edge implements the client (edge) functionality,
// using the refined protocol header for registration, heartbeat,
// data transfer, and unregistration. It retains the VIP pool management
// and other critical features.
package edge

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"n2n-go/pkg/protocol"
	"n2n-go/pkg/tuntap"
)

// EdgeClient encapsulates the state and configuration of an edge.
type EdgeClient struct {
	ID            string
	Community     string
	SupernodeAddr *net.UDPAddr
	Conn          *net.UDPConn
	TAP           *tuntap.Interface
	seq           uint16

	heartbeatInterval time.Duration

	// Use a cancellable context for shutdown.
	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	// VirtualIP as assigned by the supernode.
	VirtualIP string

	// Ensure unregister is sent only once.
	unregisterOnce sync.Once
}

// NewEdgeClient creates a new EdgeClient with a cancellable context.
func NewEdgeClient(id, community, tapName string, localPort int, supernode string, heartbeatInterval time.Duration) (*EdgeClient, error) {
	snAddr, err := net.ResolveUDPAddr("udp4", supernode)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve supernode address: %v", err)
	}
	localAddr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(localPort))
	if err != nil {
		return nil, fmt.Errorf("edge: failed to resolve local UDP address: %v", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("edge: failed to open UDP connection: %v", err)
	}
	tap, err := tuntap.NewInterface(tapName, "tap")
	if err != nil {
		return nil, fmt.Errorf("edge: failed to create TAP interface: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &EdgeClient{
		ID:                id,
		Community:         community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		seq:               0,
		heartbeatInterval: heartbeatInterval,
		ctx:               ctx,
		cancel:            cancel,
	}, nil
}

// Register sends a registration packet using the refined header.
// For registration, PacketType is TypeRegister, SourceID is the edge ID,
// and DestinationID is empty (broadcast).
func (e *EdgeClient) Register() error {
	e.seq++
	header := protocol.NewPacketHeader(3, 64, protocol.TypeRegister, e.seq, e.Community, e.ID, "")
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %v", err)
	}
	packet := append(headerBytes, []byte{}...)
	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %v", err)
	}

	// Wait for ACK.
	e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	n, addr, err := e.Conn.ReadFromUDP(buf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %v", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	parts := strings.Fields(resp)
	if len(parts) < 1 || parts[0] != "ACK" {
		return fmt.Errorf("edge: unexpected registration response from %v: %s", addr, resp)
	}
	if len(parts) >= 2 {
		e.VirtualIP = parts[1]
		log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP)
	} else {
		return fmt.Errorf("edge: registration response missing virtual IP")
	}
	e.Conn.SetReadDeadline(time.Time{})
	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	return nil
}

// Unregister sends an unregister packet (TypeUnregister).
func (e *EdgeClient) Unregister() error {
	var unregErr error
	e.unregisterOnce.Do(func() {
		e.seq++
		header := protocol.NewPacketHeader(3, 64, protocol.TypeUnregister, e.seq, e.Community, e.ID, "")
		headerBytes, err := header.MarshalBinary()
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to marshal unregister header: %v", err)
			return
		}
		packet := append(headerBytes, []byte{}...)
		_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %v", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

// runHeartbeat sends heartbeat packets periodically.
func (e *EdgeClient) runHeartbeat() {
	e.wg.Add(1)
	defer e.wg.Done()
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.seq++
			header := protocol.NewPacketHeader(3, 64, protocol.TypeHeartbeat, e.seq, e.Community, e.ID, "")
			headerBytes, err := header.MarshalBinary()
			if err != nil {
				log.Printf("Edge: Failed to marshal heartbeat header: %v", err)
				continue
			}
			packet := append(headerBytes, []byte{}...)
			_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
			if err != nil {
				log.Printf("Edge: Failed to send heartbeat: %v", err)
			}
		case <-e.ctx.Done():
			return
		}
	}
}

// runTAPToSupernode reads from the TAP interface and sends data packets.
func (e *EdgeClient) runTAPToSupernode() {
	e.wg.Add(1)
	defer e.wg.Done()
	buf := make([]byte, 1500)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		n, err := e.TAP.Read(buf)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP read error: %v", err)
			continue
		}
		e.seq++
		header := protocol.NewPacketHeader(3, 64, protocol.TypeData, e.seq, e.Community, e.ID, "")
		headerBytes, err := header.MarshalBinary()
		if err != nil {
			log.Printf("Edge: Failed to marshal data header: %v", err)
			continue
		}
		packet := append(headerBytes, buf[:n]...)
		_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending data packet: %v", err)
		}
	}
}

// runUDPToTAP reads from the UDP connection and writes payload to the TAP interface,
// discarding packets not addressed to this edge.
func (e *EdgeClient) runUDPToTAP() {
	e.wg.Add(1)
	defer e.wg.Done()
	buf := make([]byte, 1500)
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}
		n, addr, err := e.Conn.ReadFromUDP(buf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("Edge: UDP read error: %v", err)
			continue
		}
		if n < protocol.TotalHeaderSize {
			msg := strings.TrimSpace(string(buf[:n]))
			if msg == "ACK" {
				continue
			}
			log.Printf("Edge: Received packet too short from %v: %q", addr, msg)
			continue
		}
		var hdr protocol.PacketHeader
		if err := hdr.UnmarshalBinary(buf[:protocol.TotalHeaderSize]); err != nil {
			log.Printf("Edge: Failed to unmarshal header from %v: %v", addr, err)
			continue
		}
		// If DestinationID is set and does not match this edge, drop the packet.
		destID := strings.TrimRight(string(hdr.DestinationID[:]), "\x00")
		if destID != "" && destID != e.ID {
			continue
		}
		payload := buf[protocol.TotalHeaderSize:n]
		_, err = e.TAP.Write(payload)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP write error: %v", err)
		}
	}
}

// Run launches all background goroutines.
func (e *EdgeClient) Run() {
	go e.runHeartbeat()
	go e.runTAPToSupernode()
	go e.runUDPToTAP()
	<-e.ctx.Done()
	e.wg.Wait()
}

// Close initiates shutdown: it sends unregister, cancels the context, unblocks reads, and closes resources.
func (e *EdgeClient) Close() {
	if err := e.Unregister(); err != nil {
		log.Printf("Edge: Unregister failed: %v", err)
	}
	e.cancel()
	e.Conn.SetReadDeadline(time.Now())
	e.wg.Wait()
	if e.TAP != nil {
		e.TAP.Close()
	}
	if e.Conn != nil {
		e.Conn.Close()
	}
	log.Printf("Edge: Shutdown complete")
}

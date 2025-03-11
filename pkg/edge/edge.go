// Package edge implements the client (edge) functionality,
// integrating protocol framing for registration, heartbeat, unregistration,
// and data forwarding using a TAP interface.
package edge

import (
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
	done              chan struct{}  // signals overall shutdown
	wg                sync.WaitGroup // waits for all goroutines to finish

	// VirtualIP is the IP assigned by the supernode.
	VirtualIP net.IP

	// once to ensure unregister happens only once.
	unregisterOnce sync.Once
}

// NewEdgeClient creates a new EdgeClient.
// It resolves the supernode address, opens an IPv4 UDP socket, and sets up a TAP interface.
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
	return &EdgeClient{
		ID:                id,
		Community:         community,
		SupernodeAddr:     snAddr,
		Conn:              conn,
		TAP:               tap,
		seq:               0,
		heartbeatInterval: heartbeatInterval,
		done:              make(chan struct{}),
	}, nil
}

// Register sends a registration message to the supernode and parses the ACK
// to obtain the assigned virtual IP.
func (e *EdgeClient) Register() error {
	e.seq++
	header := protocol.NewPacketHeader(3, 64, 0, e.seq, e.Community)
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %v", err)
	}
	payload := []byte(fmt.Sprintf("REGISTER %s", e.ID))
	packet := append(headerBytes, payload...)
	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %v", err)
	}

	// Set a deadline for receiving the ACK.
	e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	n, addr, err := e.Conn.ReadFromUDP(buf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %v", err)
	}
	resp := strings.TrimSpace(string(buf[:n]))
	// Expected format: "ACK <virtual_ip>"
	parts := strings.Fields(resp)
	if len(parts) < 1 || parts[0] != "ACK" {
		return fmt.Errorf("edge: unexpected registration response from %v: %s", addr, resp)
	}
	if len(parts) >= 2 {
		e.VirtualIP = net.ParseIP(parts[1])
		log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP.String())
	} else {
		return fmt.Errorf("edge: registration response missing virtual IP")
	}
	// Clear the read deadline.
	e.Conn.SetReadDeadline(time.Time{})
	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	return nil
}

// Unregister sends an unregister message to the supernode so that the edge's VIP is freed.
func (e *EdgeClient) Unregister() error {
	var unregErr error
	e.unregisterOnce.Do(func() {
		e.seq++
		header := protocol.NewPacketHeader(3, 64, 0, e.seq, e.Community)
		headerBytes, err := header.MarshalBinary()
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to marshal unregister header: %v", err)
			return
		}
		payload := []byte(fmt.Sprintf("UNREGISTER %s", e.ID))
		packet := append(headerBytes, payload...)
		_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
		if err != nil {
			unregErr = fmt.Errorf("edge: failed to send unregister: %v", err)
			return
		}
		log.Printf("Edge: Unregister message sent")
	})
	return unregErr
}

// runHeartbeat sends heartbeat messages periodically.
func (e *EdgeClient) runHeartbeat() {
	e.wg.Add(1)
	defer e.wg.Done()
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.seq++
			header := protocol.NewPacketHeader(3, 64, 1, e.seq, e.Community)
			headerBytes, err := header.MarshalBinary()
			if err != nil {
				log.Printf("Edge: Failed to marshal heartbeat header: %v", err)
				continue
			}
			payload := []byte(fmt.Sprintf("HEARTBEAT %s", e.ID))
			packet := append(headerBytes, payload...)
			_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
			if err != nil {
				log.Printf("Edge: Failed to send heartbeat: %v", err)
			}
		case <-e.done:
			return
		}
	}
}

// runTAPToSupernode forwards packets from the TAP interface to the supernode.
func (e *EdgeClient) runTAPToSupernode() {
	e.wg.Add(1)
	defer e.wg.Done()
	buf := make([]byte, 1500)
	for {
		select {
		case <-e.done:
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
		header := protocol.NewPacketHeader(3, 64, 0, e.seq, e.Community)
		headerBytes, err := header.MarshalBinary()
		if err != nil {
			log.Printf("Edge: Failed to marshal header: %v", err)
			continue
		}
		prefix := []byte(fmt.Sprintf("EDGE %s ", e.ID))
		packet := append(headerBytes, prefix...)
		packet = append(packet, buf[:n]...)
		_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

// runUDPToTAP forwards packets from the UDP connection (from the supernode) to the TAP interface.
func (e *EdgeClient) runUDPToTAP() {
	e.wg.Add(1)
	defer e.wg.Done()
	buf := make([]byte, 1500)
	for {
		select {
		case <-e.done:
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
		if !hdr.VerifyTimestamp(time.Now(), 16*time.Second) {
			log.Printf("Edge: Header timestamp verification failed from %v", addr)
			continue
		}
		payload := buf[protocol.TotalHeaderSize:n]
		// Remove prefix "EDGE <edgeID> " if present.
		payloadStr := string(payload)
		if strings.HasPrefix(payloadStr, "EDGE ") {
			parts := strings.SplitN(payloadStr, " ", 3)
			if len(parts) == 3 {
				payload = []byte(parts[2])
			}
		}
		_, err = e.TAP.Write(payload)
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return
			}
			log.Printf("Edge: TAP write error: %v", err)
		}
	}
}

// Run launches the heartbeat, TAP-to-supernode, and UDP-to-TAP forwarding goroutines.
func (e *EdgeClient) Run() {
	go e.runHeartbeat()
	go e.runTAPToSupernode()
	go e.runUDPToTAP()
	// Block until done is closed.
	<-e.done
}

// Close initiates a clean shutdown: it unregisters, signals goroutines to stop,
// waits for them, then closes the TAP interface and UDP connection.
func (e *EdgeClient) Close() {
	// Attempt to unregister.
	if err := e.Unregister(); err != nil {
		log.Printf("Edge: Unregister failed: %v", err)
	}
	// Signal shutdown once.
	close(e.done)
	// Wait for all goroutines to finish.
	e.wg.Wait()
	if e.TAP != nil {
		e.TAP.Close()
	}
	if e.Conn != nil {
		e.Conn.Close()
	}
	log.Printf("Edge: Shutdown complete")
}

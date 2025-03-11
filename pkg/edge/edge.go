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

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	VirtualIP string

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

// getTapMAC retrieves the MAC address of the TAP interface using its name.
func getTapMAC(tap *tuntap.Interface) (string, error) {
	iface, err := net.InterfaceByName(tap.Name())
	if err != nil {
		return "", fmt.Errorf("failed to get interface %s: %v", tap.Name(), err)
	}
	if iface.HardwareAddr == nil || len(iface.HardwareAddr) == 0 {
		return "", fmt.Errorf("no MAC address found on interface %s", tap.Name())
	}
	return iface.HardwareAddr.String(), nil
}

// isBroadcastMAC returns true if the provided MAC address (in bytes) is the broadcast address.
func isBroadcastMAC(mac []byte) bool {
	if len(mac) != 6 {
		return false
	}
	for _, b := range mac {
		if b != 0xFF {
			return false
		}
	}
	return true
}

// formatMAC formats a 6-byte MAC address into the standard colon-separated string.
func formatMAC(mac []byte) string {
	return strings.ToUpper(fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]))
}

// Register sends a registration packet to the supernode.
// Registration payload format: "REGISTER <edgeID> <tapMAC>"
func (e *EdgeClient) Register() error {
	e.seq++
	header := protocol.NewHeader(3, 64, protocol.TypeRegister, e.seq, e.Community, e.ID, "")
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return fmt.Errorf("edge: failed to marshal registration header: %v", err)
	}
	mac, err := getTapMAC(e.TAP)
	if err != nil {
		return fmt.Errorf("edge: failed to get TAP MAC address: %v", err)
	}
	payload := []byte(fmt.Sprintf("REGISTER %s %s", e.ID, mac))
	packet := append(headerBytes, payload...)
	_, err = e.Conn.WriteToUDP(packet, e.SupernodeAddr)
	if err != nil {
		return fmt.Errorf("edge: failed to send registration: %v", err)
	}
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

// Unregister sends an unregister packet to the supernode.
func (e *EdgeClient) Unregister() error {
	var unregErr error
	e.unregisterOnce.Do(func() {
		e.seq++
		header := protocol.NewHeader(3, 64, protocol.TypeUnregister, e.seq, e.Community, e.ID, "")
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
			header := protocol.NewHeader(3, 64, protocol.TypeHeartbeat, e.seq, e.Community, e.ID, "")
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
		case <-e.ctx.Done():
			return
		}
	}
}

// runTAPToSupernode reads packets from the TAP interface and sends them to the supernode.
// It extracts the destination MAC address from the Ethernet frame and sets it in the header.
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
		if n < 14 { // Minimum Ethernet header size
			log.Printf("Edge: Packet too short to contain Ethernet header")
			continue
		}
		destMACBytes := buf[0:6]
		destMACStr := ""
		if !isBroadcastMAC(destMACBytes) {
			destMACStr = formatMAC(destMACBytes)
		}
		e.seq++
		header := protocol.NewHeader(3, 64, protocol.TypeData, e.seq, e.Community, e.ID, destMACStr)
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
			log.Printf("Edge: Error sending packet to supernode: %v", err)
		}
	}
}

// runUDPToTAP reads packets from the UDP connection and writes the payload to the TAP interface.
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
		var hdr protocol.Header
		if err := hdr.UnmarshalBinary(buf[:protocol.TotalHeaderSize]); err != nil {
			log.Printf("Edge: Failed to unmarshal header from %v: %v", addr, err)
			continue
		}
		if !hdr.VerifyTimestamp(time.Now(), 16*time.Second) {
			log.Printf("Edge: Header timestamp verification failed from %v", addr)
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

// Run launches the heartbeat, TAP-to-supernode, and UDP-to-TAP goroutines.
func (e *EdgeClient) Run() {
	go e.runHeartbeat()
	go e.runTAPToSupernode()
	go e.runUDPToTAP()
	<-e.ctx.Done()
}

// Close initiates a clean shutdown.
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

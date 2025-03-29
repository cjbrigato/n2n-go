package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/crypto"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"n2n-go/pkg/tuntap"
	"n2n-go/pkg/upnp"
	"n2n-go/pkg/util"
	"net"
	"strconv"
	"time"
)

// setupNetworkComponents initializes the UDP connection and TAP interface
func setupNetworkComponents(cfg Config) (*net.UDPConn, *tuntap.Interface, *net.UDPAddr, error) {
	snAddr, err := net.ResolveUDPAddr("udp4", cfg.SupernodeAddr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("edge: failed to resolve supernode address: %w", err)
	}

	conn, err := setupUDPConnection(cfg.LocalPort, cfg.UDPBufferSize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("edge: %w", err)
	}

	tap, err := tuntap.NewInterface(cfg.TapName, "tap")
	if err != nil {
		conn.Close() // Clean up on error
		return nil, nil, nil, fmt.Errorf("edge: failed to create TAP interface: %w", err)
	}

	return conn, tap, snAddr, nil
}

// setupUDPConnection creates and configures a UDP connection with the specified parameters
func setupUDPConnection(localPort int, bufferSize int) (*net.UDPConn, error) {
	localAddr, err := net.ResolveUDPAddr("udp4", ":"+strconv.Itoa(localPort))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve local UDP address: %w", err)
	}

	// Set larger buffer sizes for UDP
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to open UDP connection: %w", err)
	}

	// Set UDP buffer sizes to reduce latency
	if err := conn.SetReadBuffer(bufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP read buffer size: %v", err)
	}
	if err := conn.SetWriteBuffer(bufferSize); err != nil {
		log.Printf("Warning: couldn't increase UDP write buffer size: %v", err)
	}

	return conn, nil
}

func SetupUPnP(conn *net.UDPConn, edgeID string) *upnp.UPnPClient {
	udpPort := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	log.Printf("Edge: seeking for an optional UPnP/IGD<1|2> support to ease with nat traversal...")
	igdClient, err := upnp.NewUPnPClient()
	if err != nil {
		log.Printf("Edge: unable to use UPnP/IGD on this network: %v", err)
	} else {
		description := fmt.Sprintf("n2n-go.portmap for %s client", edgeID)
		leaseDuration := uint32(0)
		protocol := "udp"
		log.Printf("Edge: Discovered IGD on network ! Starting upnpClient thread...")
		log.Printf(" UPnP > Successfully connected to IGD (%s)", igdClient.GatewayType)
		log.Printf(" UPnP > Local IP: %s", igdClient.LocalIP)
		log.Printf(" UPnP > External IP: %s", igdClient.ExternalIP)
		log.Printf(" UPnP > Creating port mapping: %s %d -> %s:%d (%s)",
			"udp", udpPort, igdClient.LocalIP, udpPort, edgeID)
		err = igdClient.AddPortMapping(
			protocol,
			udpPort,
			udpPort,
			description,
			leaseDuration,
		)
		if err != nil {
			log.Printf("UPnP: Failed to add port mapping: %v :-(", err)
			igdClient = nil
		} else {
			log.Println(" UPnP > Port mapping added successfully (will be automatically deleted when edge closes)")
		}
	}
	return igdClient
}

func (e *EdgeClient) InitialSetup() error {
	if err := e.InitialGetSNPublicKey(); err != nil {
		return err
	}

	if err := e.InitialRegister(); err != nil {
		return err
	}

	if err := e.TunUp(); err != nil {
		return err
	}

	log.Printf("Edge: sending preliminary gratuitous ARP")
	if err := e.sendGratuitousARP(); err != nil {
		return err
	}

	return nil
}

// Register sends a registration packet to the supernode.
func (e *EdgeClient) InitialGetSNPublicKey() error {
	err := e.RequestSNPublicKey()
	if err != nil {
		return err
	}
	// Set a timeout for the response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Read the response
	respBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(respBuf)

	n, addr, err := e.Conn.ReadFromUDP(respBuf)
	if err != nil {
		return fmt.Errorf("edge: pubkey ACK timeout: %w", err)
	}

	// Reset deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	if n < protocol.ProtoVHeaderSize {
		return fmt.Errorf("Edge: short packet while waiting for initial SnSecretsPub")
	}

	rresp, err := protocol.MessageFromPacket[*netstruct.SNPublicSecret](respBuf, addr)
	if err != nil {
		return err
	}

	pubkey, err := crypto.PublicKeyFromPEMData(rresp.Msg.PemData)
	if err != nil {
		return err
	}
	e.SNPubKey = pubkey
	log.Printf("Got Supernode public key !")

	return nil
}

// InitialRegister sends a registration packet to the supernode.
func (e *EdgeClient) InitialRegister() error {
	err := e.RequestRegister()
	if err != nil {
		return err
	}

	// Set a timeout for the response
	if err := e.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("edge: failed to set read deadline: %w", err)
	}

	// Read the response
	respBuf := e.packetBufPool.Get()
	defer e.packetBufPool.Put(respBuf)

	n, addr, err := e.Conn.ReadFromUDP(respBuf)
	if err != nil {
		return fmt.Errorf("edge: registration ACK timeout: %w", err)
	}

	// Reset deadline
	if err := e.Conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("edge: failed to reset read deadline: %w", err)
	}

	if n < protocol.ProtoVHeaderSize {
		return fmt.Errorf("Edge: short packet while waiting for initial RegisterResponse")
	}

	rresp, err := protocol.MessageFromPacket[*netstruct.RegisterResponse](respBuf, addr)

	if err != nil {
		return err
	}

	if !rresp.Msg.IsRegisterOk {
		return ErrNACKRegister
	}
	e.VirtualIP = fmt.Sprintf("%s/%d", rresp.Msg.VirtualIP, rresp.Msg.Masklen)
	log.Printf("Edge: Assigned virtual IP %s", e.VirtualIP)
	log.Printf("Edge: Registration successful (ACK from %v)", addr)
	e.registered = true
	return nil
}

func (e *EdgeClient) sendGratuitousARP() error {
	return util.SendGratuitousARP(e.TAP.Name(), e.TAP.HardwareAddr(), net.ParseIP(e.VirtualIP))
}

func (e *EdgeClient) TunUp() error {
	if e.VirtualIP == "" {
		return fmt.Errorf("cannot configure TAP link before VirtualIP is set")
	}
	//return e.TAP.IfUp(e.VirtualIP)
	return util.IfUp(e.TAP.Name(), e.VirtualIP)
}

// Package wire handles the lower-level transport of packets using a
// ReadWriteCloser interface. It now builds and parses packet headers using the protocol package.
package wire

import (
	"log"
	"sync"
	"time"

	"n2n-go/pkg/protocol"
)

// ReadWriteCloser abstracts the underlying I/O (e.g., a TUN/TAP interface or UDP socket).
type ReadWriteCloser interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

// Handler manages packet transmission and reception using protocol framing.
type Handler struct {
	iface   ReadWriteCloser
	stop    chan struct{}
	wg      sync.WaitGroup
	seq     uint16
	seqLock sync.Mutex
}

// NewHandler creates a new Handler for the provided interface.
func NewHandler(iface ReadWriteCloser) *Handler {
	return &Handler{
		iface: iface,
		stop:  make(chan struct{}),
	}
}

// WritePacket builds a packet header using the protocol package, prepends it to the payload,
// and writes the complete packet to the underlying interface.
func (h *Handler) WritePacket(payload []byte, community string) error {
	h.seqLock.Lock()
	h.seq++
	seq := h.seq
	h.seqLock.Unlock()

	// Build header using protocol package.
	header := protocol.NewPacketHeader(3, 64, 0, seq, community)
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return err
	}

	packet := append(headerBytes, payload...)
	_, err = h.iface.Write(packet)
	return err
}

// readLoop continuously reads data from the underlying interface,
// decodes the protocol header, and processes the payload.
func (h *Handler) readLoop() {
	defer h.wg.Done()
	buf := make([]byte, 1600)
	for {
		select {
		case <-h.stop:
			return
		default:
		}
		n, err := h.iface.Read(buf)
		if err != nil {
			log.Printf("Wire read error: %v", err)
			continue
		}
		if n < protocol.TotalHeaderSize {
			log.Printf("Received packet too short: %d bytes", n)
			continue
		}

		var hdr protocol.PacketHeader
		err = hdr.UnmarshalBinary(buf[:protocol.TotalHeaderSize])
		if err != nil {
			log.Printf("Failed to unmarshal protocol header: %v", err)
			continue
		}

		// Optionally verify header timestamp for replay protection.
		if !hdr.VerifyTimestamp(time.Now(), 16*time.Second) {
			log.Printf("Header timestamp verification failed (possible replay) for seq=%d", hdr.Sequence)
			continue
		}

		payload := buf[protocol.TotalHeaderSize:n]
		h.processPacket(&hdr, payload)
	}
}

// processPacket processes the incoming packet. In a real system, you might dispatch based on type.
func (h *Handler) processPacket(hdr *protocol.PacketHeader, payload []byte) {
	log.Printf("Wire: Received packet seq=%d, community=%s, payloadLen=%d",
		hdr.Sequence, string(hdr.Community[:]), len(payload))
	// Further packet processing would go here.
}

// Start launches the read loop in a new goroutine.
func (h *Handler) Start() {
	h.wg.Add(1)
	go h.readLoop()
}

// Stop signals the read loop to terminate and waits for cleanup.
func (h *Handler) Stop() {
	close(h.stop)
	h.wg.Wait()
}

package supernode

/*
import (
	"fmt"
	"n2n-go/pkg/protocol"
)

// convertPacketFormat converts between legacy and compact header formats
func (s *Supernode) convertPacketFormat(packet []byte, toCompact bool) ([]byte, error) {
	// Parse the source header
	sourceHeader, err := protocol.ParseHeader(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source header: %w", err)
	}

	var targetHeaderSize int
	var targetHeader protocol.IHeader

	// Get payload based on source header type
	var payload []byte

	switch h := sourceHeader.(type) {
	case *protocol.Header:
		payload = packet[protocol.TotalHeaderSize:]
		if toCompact {
			// Convert Legacy -> Compact
			targetHeader = protocol.ConvertToCompactHeader(h)
			targetHeaderSize = protocol.CompactHeaderSize
		} else {
			// No conversion needed
			return packet, nil
		}

	case *protocol.CompactHeader:
		payload = packet[protocol.CompactHeaderSize:]
		if !toCompact {
			// Convert Compact -> Legacy
			targetHeader = protocol.ConvertToLegacyHeader(h)
			targetHeaderSize = protocol.TotalHeaderSize
		} else {
			// No conversion needed
			return packet, nil
		}

	default:
		return nil, fmt.Errorf("unknown header type")
	}

	// Create new packet with converted header
	newPacket := make([]byte, targetHeaderSize+len(payload))

	// Marshal new header
	if h, ok := targetHeader.(*protocol.Header); ok {
		if err := h.MarshalBinaryTo(newPacket[:protocol.TotalHeaderSize]); err != nil {
			return nil, fmt.Errorf("failed to marshal legacy header: %w", err)
		}
	} else if h, ok := targetHeader.(*protocol.CompactHeader); ok {
		if err := h.MarshalBinaryTo(newPacket[:protocol.CompactHeaderSize]); err != nil {
			return nil, fmt.Errorf("failed to marshal compact header: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unknown target header type")
	}

	// Copy payload
	copy(newPacket[targetHeaderSize:], payload)

	return newPacket, nil
}
*/

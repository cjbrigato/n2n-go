package netstruct

import "n2n-go/pkg/protocol/spec"

type Parsable interface {
	Parse(data []byte) error
	PacketType() spec.PacketType
	Encode() ([]byte, error)
}

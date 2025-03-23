package netstruct

import "n2n-go/pkg/protocol/spec"


type PacketTyped interface {
	PacketType() spec.PacketType
}

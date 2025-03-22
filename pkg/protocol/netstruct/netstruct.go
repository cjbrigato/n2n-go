package netstruct

import "n2n-go/pkg/protocol/spec"

type Messageable[T any] interface {
	Parse(data []byte) (T, error)
	Encodable
	PacketTyped
}

type Encodable interface {
	Encode() ([]byte, error)
}

type PacketTyped interface {
	PacketType() spec.PacketType
}

type Parsable[T any] interface {
	Parse(data []byte) (*T, error)
}

type GMessageable[T any] interface {
	Transformable[T]
	PacketTyped
}

type Transformable[T any] interface {
	Parsable[T]
	Encodable
}

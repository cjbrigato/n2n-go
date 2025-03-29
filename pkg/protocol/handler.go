package protocol

import (
	"fmt"
	"n2n-go/pkg/protocol/spec"
)

type MessageHandler func(r *RawMessage) error
type MessageHandlerMap map[spec.PacketType]MessageHandler

func (mh MessageHandlerMap) Handle(r *RawMessage) error {
	fn, ok := mh[r.Header.PacketType]
	if !ok {
		return fmt.Errorf("not MessageHandler found for unkown packetType: %v", r.Header.PacketType)
	}
	return fn(r)
}

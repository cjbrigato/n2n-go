package protocol

import "fmt"

type MessageHandler func(r *RawMessage) error
type MessageHandlerMap map[PacketType]MessageHandler


func (mh MessageHandlerMap) Handle(r *RawMessage) error {
	fn, ok := mh[r.Header.PacketType]
	if !ok {
		return fmt.Errorf("not MessageHandler found for: %v", r.Header.PacketType)
	}
	return fn(r)
}

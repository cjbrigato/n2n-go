package netstruct

import (
	"n2n-go/pkg/protocol/codec"
	"n2n-go/pkg/protocol/spec"
)

type RegisterRequest struct {
	EdgeMACAddr   string
	EdgeDesc      string
	CommunityName string
}

type RetryRegisterRequest struct{}

func (rreq *RegisterRequest) Encode() ([]byte, error) {
	return codec.NewCodec[RegisterRequest]().Encode(*rreq)
}

func ParseRegisterRequest(data []byte) (*RegisterRequest, error) {
	return codec.NewCodec[RegisterRequest]().Decode(data)
}

type RegisterResponse struct {
	IsRegisterOk bool
	VirtualIP    string
	Masklen      int
}
/*
func (rresp *RegisterResponse) Encode() ([]byte, error) {
	return codec.NewCodec[RegisterResponse]().Encode(*rresp)
}

func (rresp *RegisterResponse) Parse(data []byte) (*RegisterResponse, error) {
	return codec.NewCodec[RegisterResponse]().Decode(data)
}
*/


func (rresp *RegisterResponse) PacketType() spec.PacketType {
	return spec.TypeRegisterResponse
}

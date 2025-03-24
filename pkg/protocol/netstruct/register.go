package netstruct

import (
	"n2n-go/pkg/protocol/spec"
)

type SNPublicSecret struct {
	IsRequest bool
	PemData   []byte
}

func (s *SNPublicSecret) PacketType() spec.PacketType {
	return spec.TypeSNPublicSecret
}

type RegisterRequest struct {
	EdgeMACAddr        string
	EdgeDesc           string
	CommunityName      string
	EncryptedMachineID []byte
	ClearMachineID     []byte //placeholder during registration
}

type RetryRegisterRequest struct{}

func (rreq *RegisterRequest) PacketType() spec.PacketType {
	return spec.TypeRegisterRequest
}

type RegisterResponse struct {
	IsRegisterOk bool
	VirtualIP    string
	Masklen      int
}

func (rresp *RegisterResponse) PacketType() spec.PacketType {
	return spec.TypeRegisterResponse
}

type HeartbeatPulse struct {
	CommunityName string
}

func (p *HeartbeatPulse) PacketType() spec.PacketType {
	return spec.TypeHeartbeat
}

type UnregisterRequest struct {
	EdgeMACAddr   string
	CommunityName string
}

func (u *UnregisterRequest) PacketType() spec.PacketType {
	return spec.TypeUnregisterRequest
}

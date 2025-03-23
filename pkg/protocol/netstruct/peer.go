package netstruct

import "n2n-go/pkg/protocol/spec"

type PeerListRequest struct {
	CommunityName string
}

func (*PeerListRequest) PacketType() spec.PacketType {
	return spec.TypePeerListRequest
}

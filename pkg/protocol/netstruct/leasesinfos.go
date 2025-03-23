package netstruct

import (
	"n2n-go/pkg/protocol/spec"

	"github.com/cjbrigato/ippool"
)

type LeasesInfos struct {
	IsRequest     bool
	CommunityName string
	Leases        map[string]ippool.Lease //lease keyed by edge MACaddr
}

func (l *LeasesInfos) PacketType() spec.PacketType {
	return spec.TypeLeasesInfos
}

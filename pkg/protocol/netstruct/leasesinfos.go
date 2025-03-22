package netstruct

import (
	"n2n-go/pkg/protocol/codec"

	"github.com/cjbrigato/ippool"
)

type LeasesInfos struct {
	IsRequest     bool
	CommunityName string
	Leases        map[string]ippool.Lease //lease keyed by edge MACaddr
}

func (pfs *LeasesInfos) Encode() ([]byte, error) {
	return codec.NewCodec[LeasesInfos]().Encode(*pfs)
}

func ParseLeasesInfos(data []byte) (*LeasesInfos, error) {
	return codec.NewCodec[LeasesInfos]().Decode(data)
}

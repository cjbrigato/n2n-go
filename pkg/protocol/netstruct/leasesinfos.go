package netstruct

import (
	"fmt"
	"n2n-go/pkg/protocol/spec"
	"time"

	"github.com/cjbrigato/ippool"
)

type LeaseEdgeInfos struct {
	EdgeID              string
	IsRegistered        bool
	TimeSinceLastUpdate time.Duration
	//IsAlive bool
}

type LeaseWithEdgeInfos struct {
	Lease ippool.Lease
	LeaseEdgeInfos
}

type LeasesInfos struct {
	IsRequest            bool
	CommunityName        string
	LeasesWithEdgesInfos map[string]LeaseWithEdgeInfos
}

func (l *LeasesInfos) PacketType() spec.PacketType {
	return spec.TypeLeasesInfos
}

type OnlineCheck struct {
	isReply bool
	Cookie  string
}

func (*OnlineCheck) PacketType() spec.PacketType {
	return spec.TypeOnlineCheck
}

func (o *OnlineCheck) Reply() (*OnlineCheck, error) {
	if o.isReply {
		return nil, fmt.Errorf("cannot make reply of a reply")
	}
	return &OnlineCheck{
		isReply: true,
		Cookie:  o.Cookie,
	}, nil
}

func NewOnlineCheck(cookie string) *OnlineCheck {
	return &OnlineCheck{
		isReply: false,
		Cookie:  cookie,
	}
}

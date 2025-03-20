package edge

import (
	"fmt"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type EdgeClientApi struct {
	Api    *echo.Echo
	Client *EdgeClient
}

func (eapi *EdgeClientApi) GetPeers(c echo.Context) error {
	err := eapi.Client.sendP2PFullStateRequest()
	if err != nil {
		return err
	}
	for {
		if eapi.Client.Peers.IsWaitingForFullState {
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}
	state := eapi.Client.Peers.FullState
	cp2p, err := p2p.NewCommunityP2PState(eapi.Client.Community, state)
	if err != nil {
		return err
	}
	res := cp2p.GenerateP2PGraphviz()
	return c.String(http.StatusOK, res)
}

func NewEdgeApi(edge *EdgeClient) *EdgeClientApi {
	api := echo.New()
	eapi := &EdgeClientApi{
		Api:    api,
		Client: edge,
	}
	eapi.Api.GET("/peers", eapi.GetPeers, nil)
	return eapi
}

func (eapi *EdgeClientApi) Run() {
	eapi.Api.Logger.Fatal(eapi.Api.Start(":7778"))
}

func (e *EdgeClient) sendP2PFullStateRequest() error {
	req := &p2p.P2PFullState{
		CommunityName: e.Community,
		IsRequest:     true,
		FullState:     make(map[string]p2p.PeerP2PInfos),
	}
	data, err := req.Encode()
	if err != nil {
		return err
	}
	err = e.WritePacket(protocol.TypeP2PFullState, nil, string(data), p2p.UDPEnforceSupernode)
	if err != nil {
		return fmt.Errorf("edge: failed to send updated P2PInfos: %w", err)
	}
	e.Peers.IsWaitingForFullState = true
	return nil
}

package p2p

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/goccy/go-graphviz"
)

const header = `
digraph G {
    graph [fontname = "monospace" inputscale=0];
    node [fontname = "courier new" shape=underline];
    edge [fontname = "courier new" len=4.5]
   bgcolor=transparent;
   splines=true
   layout=neato
  normalize=-90
 `

func Header() string {
	return header
}

func SNPeerEdges(peersNodeIDs map[string]string) string {
	result := fmt.Sprintf("%s", "# to supernodes")
	//result = fmt.Sprintf("%s\n%s", result, "subgraph cluster1 {")
	reverse := false
	for k := range peersNodeIDs {
		if !reverse {
			result = fmt.Sprintf("%s\n\"%s\" -> \"%s\" [style=\"dashed\"  arrowhead=none, color=grey]", result, k, "sn")
		} else {
			result = fmt.Sprintf("%s\n\"%s\" -> \"%s\" [style=\"dashed\"  arrowhead=none, color=grey]", result, "sn", k)
		}
		reverse = !reverse
	}
	//result = fmt.Sprintf("%s\n%s\n", result, "}")
	result = fmt.Sprintf("%s\n", result)
	return result
}

func PeerEdges(connections map[PeerPairKey]ConnectionType) string {
	var result string
	for k, v := range connections {
		peerA, peerB, _ := k.GetPeers()
		switch v {
		case FullP2P:
			result = fmt.Sprintf("%s\n\"%s\" -> \"%s\"[dir=both,style=bold, color=green]", result, peerA, peerB)
		case PartialP2P_AtoB:
			result = fmt.Sprintf("%s\n\"%s\" -> \"%s\"[color=orange,style=bold]", result, peerA, peerB)
		case PartialP2P_BtoA:
			result = fmt.Sprintf("%s\n\"%s\" -> \"%s\"[color=orange,style=bold]", result, peerB, peerA)
		}
	}
	result = fmt.Sprintf("%s\n", result)
	return result
}

func PeerNodes(peersIdLabels map[string]string) string {
	result := fmt.Sprintf("%s", "# supernode def")
	result = fmt.Sprintf("%s\n%s", result, "\"sn\" [shape=rectangle,style=\"rounded,bold\" color=\"#FFB0B0\" label=\"SUPER\\nNODE\" pos=\"60,0!\"]\n\n # nodedefs")
	for k, v := range peersIdLabels {
		result = fmt.Sprintf("%s\n \"%s\" [color=grey label=\"💻%s\\n%s\"]", result, k, v, k)
	}
	result = fmt.Sprintf("%s\n%s\n", result, "}")
	return result
}

// Connection type between two peers
type ConnectionType int

const (
	FullP2P ConnectionType = iota
	PartialP2P_AtoB
	PartialP2P_BtoA
	NoP2P
)

// Build a map of peer connection status
type ConnectionInfo struct {
	Status   P2PCapacity
	FromPeer string
	ToPeer   string
}

type PeerDirectedPairKey string

func NewPeerDirectedPairKey(from, to string) PeerDirectedPairKey {
	return PeerDirectedPairKey(from + "->" + to)
}

func (pdpk PeerDirectedPairKey) GetDirectedPeers() (from, to string, err error) {
	peers := strings.Split(string(pdpk), "->")
	if len(peers) < 2 {
		err = fmt.Errorf("bogus cannot decode bogus PeerDirectedPairKey")
		return
	}
	from = peers[0]
	to = peers[1]
	return
}

func (pdpk PeerDirectedPairKey) ToPeerPairKey() (PeerPairKey, error) {
	from, to, err := pdpk.GetDirectedPeers()
	if err != nil {
		return PeerPairKey(""), err
	}
	return NewPeerPairKey(from, to), nil
}

type PeerPairKey string

func NewPeerPairKey(peerA, peerB string) PeerPairKey {
	pairKey := ""
	if peerA < peerB {
		pairKey = peerA + "," + peerB
	} else {
		pairKey = peerB + "," + peerA
	}
	return PeerPairKey(pairKey)
}

func (ppk PeerPairKey) GetPeers() (peerA, peerB string, err error) {
	peers := strings.Split(string(ppk), ",")
	if len(peers) < 2 {
		err = fmt.Errorf("bogus cannot decode bogus PeerPairKey")
		return
	}
	peerA = peers[0]
	peerB = peers[1]
	return
}

type CommunityP2PState struct {
	CommunityName        string
	PeersDescToVIP       map[string]string
	P2PAvailabilityInfos map[string]PeerP2PInfos
	ConnectionData       map[PeerDirectedPairKey]ConnectionInfo
	PeerPairs            map[PeerPairKey]bool
	P2PStates            map[PeerPairKey]ConnectionType
}

func NewCommunityP2PState(community string, peerInfos map[string]PeerP2PInfos) (*CommunityP2PState, error) {
	peersDescToVIP := make(map[string]string)
	connectionData := make(map[PeerDirectedPairKey]ConnectionInfo)
	peerPairs := make(map[PeerPairKey]bool)
	P2PStates := make(map[PeerPairKey]ConnectionType)

	// connectionData
	for _, peerInfo := range peerInfos {
		fromID := peerInfo.From.Infos.Desc
		fromVIP := peerInfo.From.Infos.VirtualIP.String()
		peersDescToVIP[fromID] = fromVIP

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.Desc
			connKey := NewPeerDirectedPairKey(fromID, toID)
			connectionData[connKey] = ConnectionInfo{
				Status:   toPeer.P2PStatus,
				FromPeer: fromID,
				ToPeer:   toID,
			}
		}
	}

	// peerPairs
	for connKey := range connectionData {
		pairKey, err := connKey.ToPeerPairKey()
		if err != nil {
			return nil, err
		}
		peerPairs[pairKey] = true
	}

	//P2PStates
	for pairKey := range peerPairs {
		peerA, peerB, err := pairKey.GetPeers()
		if err != nil {
			return nil, err
		}
		// Determine the connection type
		connType := GetConnectionType(peerA, peerB, connectionData)
		P2PStates[pairKey] = connType
	}

	return &CommunityP2PState{
		CommunityName:        community,
		PeersDescToVIP:       peersDescToVIP,
		P2PAvailabilityInfos: peerInfos,
		ConnectionData:       connectionData,
		PeerPairs:            peerPairs,
		P2PStates:            P2PStates,
	}, nil
}

func GetConnectionType(peerA, peerB string, connectionData map[PeerDirectedPairKey]ConnectionInfo) ConnectionType {
	aToB, hasAtoB := connectionData[NewPeerDirectedPairKey(peerA, peerB)]
	bToA, hasBtoA := connectionData[NewPeerDirectedPairKey(peerB, peerA)]

	aToBisP2P := hasAtoB && aToB.Status == P2PAvailable
	bToAisP2P := hasBtoA && bToA.Status == P2PAvailable

	if aToBisP2P && bToAisP2P {
		return FullP2P
	} else if aToBisP2P && !bToAisP2P {
		return PartialP2P_AtoB
	} else if !aToBisP2P && bToAisP2P {
		return PartialP2P_BtoA
	} else {
		return NoP2P
	}
}

func (cs *CommunityP2PState) GenerateP2PGraphviz() string {
	result := Header()
	result = fmt.Sprintf("%s\n%s", result, SNPeerEdges(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n%s", result, PeerEdges(cs.P2PStates))
	result = fmt.Sprintf("%s\n%s", result, PeerNodes(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n", result)
	return result
}

func (cs *CommunityP2PState) GenerateP2PGraphImage() ([]byte, error) {
	data := []byte(cs.GenerateP2PGraphviz())
	graph, err := graphviz.ParseBytes(data)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = g.Render(ctx, graph, graphviz.SVG, &buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

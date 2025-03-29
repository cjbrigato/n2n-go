package p2p

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-graphviz"
)


func add_offline(s string, desc string, ip string) string {
	return fmt.Sprintf("%s\n\"%s\" [color=grey label=<<table BORDER=\"0\" CELLBORDER=\"0\"><tr><TD ROWSPAN=\"3\"><img src=\"/static/cloud.png\"/></TD><td align=\"left\">%s</td></tr><tr><TD align=\"left\">%s</TD></tr></table>>]", s, desc, desc, ip)
}


func genHeader(community string) string {
	return fmt.Sprintf(header, strings.ToTitle(community))
}

func snPeerEdges(peersNodeIDs map[string]string) string {
	result := fmt.Sprintf("%s", "# to supernodes")
	//result = fmt.Sprintf("%s\n%s", result, "subgraph cluster1 {")
	reverse := false

	keys := make([]string, 0, len(peersNodeIDs))
	for k := range peersNodeIDs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
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

func P2VizGenOfflinesDot(offlines map[string]PeerCachedInfo) string {
	var offnodes string
	keys := make([]string, 0, len(offlines))
	for k := range offlines {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		offnodes = add_offline(offnodes, offlines[k].Desc, offlines[k].VirtualIP.String())
	}
	return fmt.Sprintf(offlinegraph, offnodes)
}

func peerEdges(connections map[PeerPairKey]ConnectionType) string {
	var result string
	keys := make([]string, 0, len(connections))
	for k := range connections {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	//for k, v := range connections {
	for _, k := range keys {
		v := connections[PeerPairKey(k)]
		peerA, peerB, _ := PeerPairKey(k).getPeers()
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

func peerNodes(peersIdLabels map[string]string) string {
	result := fmt.Sprintf("%s", "# supernode def")
	result = fmt.Sprintf("%s\n%s", result, "\"sn\" [shape=rectangle,style=\"rounded,bold\" color=\"#FFB0B0\" label=\"SUPER\\nNODE\" pos=\"60,0!\"]\n\n # nodedefs")
	keys := make([]string, 0, len(peersIdLabels))
	for k := range peersIdLabels {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		result = fmt.Sprintf("%s\n \"%s\" [color=grey label=\"💻%s\\n%s\"]", result, k, peersIdLabels[k], k)
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

func newPeerDirectedPairKey(from, to string) PeerDirectedPairKey {
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
	return newPeerPairKey(from, to), nil
}

type PeerPairKey string

func newPeerPairKey(peerA, peerB string) PeerPairKey {
	pairKey := ""
	if peerA < peerB {
		pairKey = peerA + "," + peerB
	} else {
		pairKey = peerB + "," + peerA
	}
	return PeerPairKey(pairKey)
}

func (ppk PeerPairKey) getPeers() (peerA, peerB string, err error) {
	peers := strings.Split(string(ppk), ",")
	if len(peers) < 2 {
		err = fmt.Errorf("bogus cannot decode bogus PeerPairKey")
		return
	}
	peerA = peers[0]
	peerB = peers[1]
	return
}

type CommunityP2PVizDatas struct {
	CommunityName        string
	PeersDescToVIP       map[string]string
	P2PAvailabilityInfos map[string]PeerP2PInfos
	ConnectionData       map[PeerDirectedPairKey]ConnectionInfo
	PeerPairs            map[PeerPairKey]bool
	P2PStates            map[PeerPairKey]ConnectionType
}

func NewCommunityP2PVizDatas(community string, peerInfos map[string]PeerP2PInfos) (*CommunityP2PVizDatas, error) {
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
			connKey := newPeerDirectedPairKey(fromID, toID)
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
		peerA, peerB, err := pairKey.getPeers()
		if err != nil {
			return nil, err
		}
		// Determine the connection type
		connType := getConnectionType(peerA, peerB, connectionData)
		P2PStates[pairKey] = connType
	}

	return &CommunityP2PVizDatas{
		CommunityName:        community,
		PeersDescToVIP:       peersDescToVIP,
		P2PAvailabilityInfos: peerInfos,
		ConnectionData:       connectionData,
		PeerPairs:            peerPairs,
		P2PStates:            P2PStates,
	}, nil
}

func getConnectionType(peerA, peerB string, connectionData map[PeerDirectedPairKey]ConnectionInfo) ConnectionType {
	aToB, hasAtoB := connectionData[newPeerDirectedPairKey(peerA, peerB)]
	bToA, hasBtoA := connectionData[newPeerDirectedPairKey(peerB, peerA)]

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

func (cs *CommunityP2PVizDatas) GenerateP2PGraphviz() string {
	result := genHeader(cs.CommunityName)
	result = fmt.Sprintf("%s\n%s", result, snPeerEdges(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n%s", result, peerEdges(cs.P2PStates))
	result = fmt.Sprintf("%s\n%s", result, peerNodes(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n", result)
	return result
}

func (cs *CommunityP2PVizDatas) GenerateP2PGraphImage() ([]byte, error) {
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

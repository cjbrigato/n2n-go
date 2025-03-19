package p2p

import (
	"fmt"
	"log"
	"strings"
)

const header = `
digraph G {
	graph [fontname = "monospace"];
	node [fontname = "monospace"];
	edge [fontname = "monospace"];
  
   bgcolor=transparent;
   damping=0.99
   force=12
   splines=curved
   layout=neato
   root=false
   pin=false
   overlap=false
   scale=1.5
   ratio=.7
   sep=.3
   start=232334
   model=subset
 `

func SNPeerEdges(peersNodeIDs []string) string {
	result := fmt.Sprintf("%s", "# to supernodes")
	result = fmt.Sprintf("%s\n%s", result, "subgraph cluster1 {")
	reverse := false
	for _, p := range peersNodeIDs {
		if !reverse {
			result = fmt.Sprintf("%s\n%s -> %s [style=\"dashed,bold\"  arrowhead=none, color=grey]", result, p, "sn")
		} else {
			result = fmt.Sprintf("%s\n%s -> %s [style=\"dashed,bold\"  arrowhead=none, color=grey]", result, "sn", p)
		}
		reverse = !reverse
	}
	result = fmt.Sprintf("%s\n%s\n", result, "}")
	return result
}

func PeerNodes(peersIdLabels map[string]string) string {
	result := fmt.Sprintf("%s", "# supernode def")
	result = fmt.Sprintf("%s\n%s", result, "sn [shape=rectangle,style=\"rounded,bold\" color=\"#FFB0B0\"  fontsize=25 label=\"SUPER\\nNODE\" pos=\"20,101,1,5.0,0.5,0.5\"]\n\n # nodedefs")
	for k, v := range peersIdLabels {
		result = fmt.Sprintf("%s\n %s [shape=rectangle color=grey label=\"💻%s\" fontsize=25 style=\"bold,dashed\"]", result, k, v)
	}
	result = fmt.Sprintf("%s\n%s\n", result, "}")
	return result
}

/*
func P2PEdges(cs *CommunityP2PState) string {
	for k, v := range cs.p2pAvailablityDatas {
		pA, exists := cs.CommunityPeers[k]
		if !exists {
			log.Printf("peer %s does not exists in cs.CommunityPeers, skipping", k)
			continue
		}
		for kk, vv := range cs.p2pAvailablityDatas[k] {

		}

	}
}*/

type PeerP2PInfos struct {
	From *Peer
	To   []*Peer
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

type CommunityP2PState struct {
	CommunityName       string
	p2pAvailablityDatas map[string]map[string]bool
	CommunityPeers      map[string]PeerInfo
}

func (cps *CommunityP2PState) GetPeerNodeIDs() []string {
	keys := make([]string, len(cps.CommunityPeers))
	i := 0
	for k := range cps.CommunityPeers {
		keys[i] = k
		i++
	}
	return keys
}

func (cps *CommunityP2PState) PeerIDtoPeerInfos(id string) (*PeerInfo, error) {
	pA, exists := cps.CommunityPeers[id]
	if !exists {
		log.Printf("peer %s does not exists in cs.CommunityPeers, skipping", id)
		return nil, fmt.Errorf("peer %s does not exists in cs.CommunityPeers", id)
	}
	return &pA, nil
}

func (cps *CommunityP2PState) PeerIDtoReachablePeerIDs(id string) (map[string]bool, error) {
	pi, exists := cps.p2pAvailablityDatas[id]
	if !exists {
		log.Printf("peer %s does not exists in cs.p2pAvailablityDatas, skipping", id)
		return nil, fmt.Errorf("peer %s does not exists in cs.p2pAvailablityDatas", id)
	}
	return pi, nil
}

func GenerateConnectionDatas(peerInfos []PeerP2PInfos) map[string]ConnectionInfo {
	connectionData := make(map[string]ConnectionInfo)

	// Collect all connection data
	for _, peerInfo := range peerInfos {
		fromID := peerInfo.From.Infos.VirtualIP.String()

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()
			connKey := fromID + "->" + toID
			connectionData[connKey] = ConnectionInfo{
				Status:   toPeer.P2PStatus,
				FromPeer: fromID,
				ToPeer:   toID,
			}
		}
	}

	return connectionData
}

func GetConnectionType(peerA, peerB string, connectionData map[string]ConnectionInfo) ConnectionType {
	aToB, hasAtoB := connectionData[peerA+"->"+peerB]
	bToA, hasBtoA := connectionData[peerB+"->"+peerA]

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

func ProcessConnectionData(connectionData map[string]ConnectionInfo) {

	// Create a set of all peer pairs we need to analyze
	peerPairs := make(map[string]bool)
	for connKey := range connectionData {
		parts := strings.Split(connKey, "->")
		peerA := parts[0]
		peerB := parts[1]

		// Create a canonical key for the pair (sorted by string to ensure uniqueness)
		pairKey := ""
		if peerA < peerB {
			pairKey = peerA + "," + peerB
		} else {
			pairKey = peerB + "," + peerA
		}

		peerPairs[pairKey] = true
	}

	for pairKey := range peerPairs {
		peers := strings.Split(pairKey, ",")
		peerA := peers[0]
		peerB := peers[1]

		// Determine the connection type
		connType := GetConnectionType(peerA, peerB, connectionData)

		switch connType {
		case FullP2P:

		case PartialP2P_AtoB:
			// Partial P2P (A->B direct, B->A via supernode)
			// Direct connection A->B

		case PartialP2P_BtoA:
			// Partial P2P (B->A direct, A->B via supernode)
			// Direct connection B->A

		case NoP2P:

		}
	}

}

/*
   # full
   raki -> colin[dir=both,style=bold, color=green,label=""]
   tsd -> krali[dir=both,style=bold, color=green,label=""]
   raki -> krali[dir=both,style=bold, color=green,label=""]

   # partials
   raki -> tsd[color=orange,style=bold]
   tsd -> colin[color=orange,style=bold]
   colin -> krali[color=orange,style=bold]
*/

package edge

import (
	"fmt"
	"n2n-go/pkg/peer"
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

type CommunityP2PState struct {
	CommunityName       string
	p2pAvailablityDatas map[string]map[string]bool
	CommunityPeers      map[string]peer.PeerInfo
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

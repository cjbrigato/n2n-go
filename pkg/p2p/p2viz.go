package p2p

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-graphviz"
)

const peersHTML = `
<!DOCTYPE html>
<meta charset="utf-8">
<style>
.grid-container {
  display: grid;
  grid-template-columns: 10%% 55%% 35%%;
}
</style>
<body>
<script src="//d3js.org/d3.v7.min.js"></script>
<script src="https://unpkg.com/@hpcc-js/wasm@2.20.0/dist/graphviz.umd.js"></script>
<script src="https://unpkg.com/d3-graphviz@5.6.0/build/d3-graphviz.js"></script>
<div class="grid-container">
<div></div>
<div id="peers" style="text-align: center;"></div>
<div id="offlines" style="text-align: left;"></div>
</div>
<div class="grid-container">
<div></div>
<div id="legend" style="text-align: center;"></div>
<div></div>
</div>
<script>
var dot = ""
var dotoff = ""
var graphviz = d3.select("#peers").graphviz()
    .transition(function () {
        return d3.transition("main")
            .ease(d3.easeLinear)
            .delay(500)
            .duration(1500);
    })
    .logEvents(true)
    .on("initEnd", render);

	var graphvizOff = d3.select("#offlines").graphviz()
    .transition(function () {
        return d3.transition("main")
            .ease(d3.easeLinear)
            .delay(500)
            .duration(1500);
    })
    .logEvents(true)
    .on("initEnd", renderOff);

	function renderOff() {
		console.log(dotoff)
		graphvizOff.addImage("/static/cloud.png","32px","32px")
			.renderDot(dotoff)
			.on("end", function () {
				renderOff();
			});
	}

	function render() {
		console.log(dot)
		graphviz.addImage("/static/cloud.png","32px","32px")
			.renderDot(dot)
			.on("end", function () {
				render();
			});
	}
	

let intervalId 
const req = new XMLHttpRequest();
const reqoff = new XMLHttpRequest();
req.onreadystatechange = function(){
    "use strict";
    if(req.readyState === 4 && req.status === 200){
        dot=req.responseText;
		render()
    }
};
reqoff.onreadystatechange = function(){
    "use strict";
    if(reqoff.readyState === 4 && reqoff.status === 200){
        dotoff=reqoff.responseText;
		renderOff()
    }
};

setInterval(update, 2000);

function update(){
req.open("GET", "/peers.dot");
req.send();
reqoff.open("GET", "/offlines.dot");
reqoff.send();
}

d3.select("#offlines").graphviz().renderDot(%s);

d3.select("#peers").graphviz()
    .renderDot(%s);
d3.select("#legend").graphviz()
    .renderDot(%s);

</script>
`

const offlinegraph = `
digraph G {
    rankdir=LR
    graph [fontname = "courier new" inputscale=0];
    labelloc="t"
    fontsize=16
    center=true
    node [fontname = "courier new" fontsize=9 shape=plain];
    edge [fontname = "courier new" len=4.5]
   bgcolor=transparent;
 fontsize=9
 fontname = "courier new"
 
 rank = same {
 %s
 }
}
`

func add_offline(s string, desc string, ip string) string {
	return fmt.Sprintf("%s\n\"%s\" [color=grey label=<<table BORDER=\"0\" CELLBORDER=\"0\"><tr><TD ROWSPAN=\"3\"><img src=\"/static/cloud.png\"/></TD><td align=\"left\">%s</td></tr><tr><TD align=\"left\">%s</TD></tr></table>>]", s, desc, desc, ip)
}

const legend = `
digraph G {
    rankdir=LR
	
    node [fontname = "courier new"];
    edge [fontsize=11 fontname="courier new"];
 
  subgraph cluster_1 {
    fontsize=11
    fontname = "courier new"
    node [shape=plain];
    A -> B [label="Supernode I/O (no P2P)" style="dashed"  arrowhead=none, color=grey len=3.0]
    C -> D [label="Half Direct Connection" color=orange,style=bold len=3.0]
    E -> F [label="Full Duplex P2P" dir=both,style=bold, color=green]
    label = "Legend";
  }
  A [label=" "]
  B [label=" "]
  C [label=" "]
  D [label=" "]
  E [label=" "]
  F [label=" "]
}
`

const header = `
digraph G {
    graph [fontname = "courier new" inputscale=0];
    label=<<font point-size="22">Network<br align="center"/>::<b>%s</b>::<br align="center"/></font>>
    labelloc="t"
    fontsize=28
    center=true
    node [fontname = "courier new" fontsize=11 shape=underline];
    edge [fontname = "courier new" len=4.5]

   bgcolor=transparent;
   splines=true
   layout=neato
  normalize=-90
 `

func Header(community string) string {
	return fmt.Sprintf(header, community)
}

func SNPeerEdges(peersNodeIDs map[string]string) string {
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

func GenOfflines(offlines map[string]PeerCachedInfo) string {
	var offnodes string
	for _, v := range offlines {
		offnodes = add_offline(offnodes, v.Desc, v.VirtualIP.String())
	}
	return fmt.Sprintf(offlinegraph, offnodes)
}

func PeerEdges(connections map[PeerPairKey]ConnectionType) string {
	var result string
	keys := make([]string, 0, len(connections))
	for k := range connections {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	//for k, v := range connections {
	for _, k := range keys {
		v := connections[PeerPairKey(k)]
		peerA, peerB, _ := PeerPairKey(k).GetPeers()
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

type CommunityP2PVizDatas struct {
	CommunityName        string
	PeersDescToVIP       map[string]string
	P2PAvailabilityInfos map[string]PeerP2PInfos
	ConnectionData       map[PeerDirectedPairKey]ConnectionInfo
	PeerPairs            map[PeerPairKey]bool
	P2PStates            map[PeerPairKey]ConnectionType
	Offlines             map[string]PeerCachedInfo
}

func NewCommunityP2PVizDatas(community string, peerInfos map[string]PeerP2PInfos, offlines map[string]PeerCachedInfo) (*CommunityP2PVizDatas, error) {
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

	return &CommunityP2PVizDatas{
		CommunityName:        community,
		PeersDescToVIP:       peersDescToVIP,
		P2PAvailabilityInfos: peerInfos,
		ConnectionData:       connectionData,
		PeerPairs:            peerPairs,
		P2PStates:            P2PStates,
		Offlines:             offlines,
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

func (cs *CommunityP2PVizDatas) GenerateP2POfflinesGraphviz() string {
	return GenOfflines(cs.Offlines)
}

func (cs *CommunityP2PVizDatas) GenerateP2PGraphviz() string {
	result := Header(cs.CommunityName)
	result = fmt.Sprintf("%s\n%s", result, SNPeerEdges(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n%s", result, PeerEdges(cs.P2PStates))
	result = fmt.Sprintf("%s\n%s", result, PeerNodes(cs.PeersDescToVIP))
	result = fmt.Sprintf("%s\n", result)
	return result
}

func (cs *CommunityP2PVizDatas) GenerateP2PGraphvizLegend() string {
	return legend
}

func (cs *CommunityP2PVizDatas) GenerateP2PHTML() string {
	graph := fmt.Sprintf("`%s`", cs.GenerateP2PGraphviz())
	legraph := fmt.Sprintf("`%s`", cs.GenerateP2PGraphvizLegend())
	offgraph := fmt.Sprintf("`%s`", cs.GenerateP2POfflinesGraphviz())
	result := fmt.Sprintf(peersHTML, offgraph, graph, legraph)
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

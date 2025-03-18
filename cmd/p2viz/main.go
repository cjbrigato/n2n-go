/*package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Type definitions from your code
type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

func (pt P2PCapacity) String() string {
	switch pt {
	case P2PUnknown:
		return "Unknown"
	case P2PPending:
		return "Pending"
	case P2PAvailable:
		return "Available"
	case P2PUnavailable:
		return "Unavailable"
	default:
		return "Unknown"
	}
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	Community string           `json:"community"`
	Desc      string           `json:"desc"`
}

type Peer struct {
	Infos      PeerInfo
	P2PStatus  P2PCapacity
	P2PCheckID string
	pendingTTL int
	UpdatedAt  time.Time
}

type PeerP2PInfos struct {
	From *Peer
	To   []*Peer
}

// GenerateP2PGraphviz creates a Graphviz DOT file representation of peer-to-peer connections
func GenerateP2PGraphviz(peerInfos []PeerP2PInfos, supernodeName string) (string, error) {
	// Create a new directed graph
	var g strings.Builder
	g.WriteString("digraph P2PNetwork {\n")
	g.WriteString("  // Graph settings\n")
	g.WriteString("  rankdir=LR;\n") // Left to right layout
	g.WriteString("  node [shape=box, style=\"rounded,filled\", fillcolor=lightblue];\n")
	g.WriteString("  // Legend\n")
	g.WriteString("  subgraph cluster_legend {\n")
	g.WriteString("    label=\"Legend\";\n")
	g.WriteString("    style=filled;\n")
	g.WriteString("    color=lightgrey;\n")
	g.WriteString("    \"P2P Direct\" [shape=plaintext, label=\"→ Direct P2P connection\"];\n")
	g.WriteString("    \"Via Supernode\" [shape=plaintext, label=\"- - → Via Supernode\"];\n")
	g.WriteString("    \"Bidirectional P2P\" [shape=plaintext, label=\"⟷ Full P2P (both directions)\"];\n")
	g.WriteString("  }\n\n")

	// Add supernode
	g.WriteString("  // Supernode\n")
	g.WriteString(fmt.Sprintf("  \"%s\" [shape=ellipse, fillcolor=lightgreen];\n", supernodeName))
	g.WriteString("\n")

	// Map to keep track of added nodes (to avoid duplicates)
	addedNodes := make(map[string]bool)

	// Add all peers as nodes
	g.WriteString("  // Peer nodes\n")
	for _, peerInfo := range peerInfos {
		fromPeer := peerInfo.From
		fromID := fromPeer.Infos.VirtualIP.String()

		if !addedNodes[fromID] {
			g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
				fromID,
				fromID,
				fromPeer.Infos.Desc))
			addedNodes[fromID] = true
		}

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()

			if !addedNodes[toID] {
				g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
					toID,
					toID,
					toPeer.Infos.Desc))
				addedNodes[toID] = true
			}
		}
	}
	g.WriteString("\n")

	// Create a map to store connection status between pairs of peers
	connectionStatus := make(map[string]P2PCapacity)

	// Collect all connection statuses
	for _, peerInfo := range peerInfos {
		fromID := peerInfo.From.Infos.VirtualIP.String()

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()
			connKey := fromID + "->" + toID
			connectionStatus[connKey] = toPeer.P2PStatus
		}
	}

	// Track rendered connections to avoid duplicates
	renderedConnections := make(map[string]bool)

	// First pass: Check for bidirectional P2P connections
	g.WriteString("  // Bidirectional P2P connections\n")
	for connKey, status := range connectionStatus {
		if status != P2PAvailable {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		// Check for reverse direction
		reverseKey := toID + "->" + fromID
		reverseStatus, exists := connectionStatus[reverseKey]

		// If both directions are P2PAvailable and we haven't rendered either yet
		if exists && reverseStatus == P2PAvailable && !renderedConnections[connKey] && !renderedConnections[reverseKey] {
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [dir=both, color=green, penwidth=2.0, label=\"Full P2P\"];\n",
				fromID, toID))
			renderedConnections[connKey] = true
			renderedConnections[reverseKey] = true
		}
	}
	g.WriteString("\n")

	// Second pass: Handle one-way P2P connections
	g.WriteString("  // One-way P2P connections\n")
	for connKey, status := range connectionStatus {
		if status != P2PAvailable || renderedConnections[connKey] {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=blue, penwidth=1.5, label=\"P2P\"];\n",
			fromID, toID))
		renderedConnections[connKey] = true
	}
	g.WriteString("\n")

	// Third pass: Handle connections through the supernode
	g.WriteString("  // Connections through supernode\n")

	// Track supernode connections to avoid duplicates
	peerToSuper := make(map[string]bool)
	superToPeer := make(map[string]bool)

	for connKey, status := range connectionStatus {
		if status == P2PAvailable || renderedConnections[connKey] {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		// Connection from peer to supernode
		if !peerToSuper[fromID] {
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=red, style=dashed];\n",
				fromID, supernodeName))
			peerToSuper[fromID] = true
		}

		// Connection from supernode to peer
		if !superToPeer[toID] {
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=red, style=dashed];\n",
				supernodeName, toID))
			superToPeer[toID] = true
		}
	}

	g.WriteString("}\n")
	return g.String(), nil
}

// SaveP2PGraphvizToFile saves the generated DOT representation to a file
func SaveP2PGraphvizToFile(peerInfos []PeerP2PInfos, supernodeName, filePath string) error {
	dotContent, err := GenerateP2PGraphviz(peerInfos, supernodeName)
	if err != nil {
		return err
	}

	// Write to file (requires "os" import)
	// return os.WriteFile(filePath, []byte(dotContent), 0644)

	// This is a placeholder until you import "os"
	fmt.Println("Generated DOT file:")
	fmt.Println(dotContent)
	return nil
}

// Example of how to use this function
func main() {
	// Example data setup
	peer1 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.1"),
			Desc:      "Peer 1",
		},
	}

	peer2 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.2"),
			Desc:      "Peer 2",
		},
		P2PStatus: P2PAvailable,
	}

	peer3 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.3"),
			Desc:      "Peer 3",
		},
		P2PStatus: P2PPending,
	}

	// Create peer info collection
	peerInfos := []PeerP2PInfos{
		{
			From: peer1,
			To: []*Peer{
				peer2,
				peer3,
			},
		},
		{
			From: peer2,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PUnavailable,
				},
			},
		},
		{
			From: peer3,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
	}

	// Generate the DOT file
	SaveP2PGraphvizToFile(peerInfos, "SuperNode", "p2p_network.dot")
}
*/
/*
package main

import (
	"context"
	"fmt"
	_ "image/color"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"
)

// Type definitions from your code
type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

func (pt P2PCapacity) String() string {
	switch pt {
	case P2PUnknown:
		return "Unknown"
	case P2PPending:
		return "Pending"
	case P2PAvailable:
		return "Available"
	case P2PUnavailable:
		return "Unavailable"
	default:
		return "Unknown"
	}
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	Community string           `json:"community"`
	Desc      string           `json:"desc"`
}

type Peer struct {
	Infos      PeerInfo
	P2PStatus  P2PCapacity
	P2PCheckID string
	pendingTTL int
	UpdatedAt  time.Time
}

type PeerP2PInfos struct {
	From *Peer
	To   []*Peer
}

// GenerateP2PGraph creates a graphviz graph representation of peer-to-peer connections
func GenerateP2PGraph(peerInfos []PeerP2PInfos, supernodeName string) (*graphviz.Graphviz, *cgraph.Graph, error) {
	g, err := graphviz.New(context.Background())
	if err != nil {
		return nil, nil, err
	}
	graph, err := g.Graph(graphviz.Directed)
	if err != nil {
		return nil, nil, err
	}

	// Set graph attributes
	graph.SetRankDir(cgraph.LRRank) // Left to right layout
	graph.SetLabel("P2P Network Connections")

	// Create a legend subgraph
	legend, err := graph.SubGraph("cluster_legend", cgraph.ClusterSubGraph)
	if err != nil {
		return nil, nil, err
	}
	legend.SetLabel("Legend")
	legend.SetBackgroundColor("lightgrey")

	directP2P, err := legend.CreateNode("Direct P2P")
	if err != nil {
		return nil, nil, err
	}
	directP2P.SetShape(cgraph.PlainTextShape)
	directP2P.SetLabel("→ Direct P2P connection")

	viaSuper, err := legend.CreateNode("Via Supernode")
	if err != nil {
		return nil, nil, err
	}
	viaSuper.SetShape(cgraph.PlainTextShape)
	viaSuper.SetLabel("- - → Via Supernode")

	biDirect, err := legend.CreateNode("Bidirectional P2P")
	if err != nil {
		return nil, nil, err
	}
	biDirect.SetShape(cgraph.PlainTextShape)
	biDirect.SetLabel("⟷ Full P2P (both directions)")

	// Add supernode
	supernode, err := graph.CreateNode(supernodeName)
	if err != nil {
		return nil, nil, err
	}
	supernode.SetShape(cgraph.EllipseShape)
	supernode.SetFillColor("lightgreen")

	// Map to keep track of nodes and their graphviz node objects
	nodes := make(map[string]*cgraph.Node)

	// Add supernode to the nodes map
	nodes[supernodeName] = supernode

	// Add all peers as nodes
	for _, peerInfo := range peerInfos {
		fromPeer := peerInfo.From
		fromID := fromPeer.Infos.VirtualIP.String()

		// Add from peer if not already added
		if _, exists := nodes[fromID]; !exists {
			node, err := graph.CreateNode(fromID)
			if err != nil {
				return nil, nil, err
			}

			node.SetShape(cgraph.BoxShape)
			node.SetStyle(cgraph.FilledNodeStyle)
			node.SetFillColor("lightblue")
			node.SetLabel(fmt.Sprintf("%s\n%s", fromID, fromPeer.Infos.Desc))

			nodes[fromID] = node
		}

		// Add to peers if not already added
		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()

			if _, exists := nodes[toID]; !exists {
				node, err := graph.CreateNode(toID)
				if err != nil {
					return nil, nil, err
				}

				node.SetShape(cgraph.BoxShape)
				node.SetStyle(cgraph.FilledNodeStyle)
				node.SetFillColor("lightblue")
				node.SetLabel(fmt.Sprintf("%s\n%s", toID, toPeer.Infos.Desc))

				nodes[toID] = node
			}
		}
	}

	// Create a map to store connection status between pairs of peers
	connectionStatus := make(map[string]P2PCapacity)

	// Collect all connection statuses
	for _, peerInfo := range peerInfos {
		fromID := peerInfo.From.Infos.VirtualIP.String()

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()
			connKey := fromID + "->" + toID
			connectionStatus[connKey] = toPeer.P2PStatus
		}
	}

	// Track rendered connections to avoid duplicates
	renderedConnections := make(map[string]bool)

	// First pass: Check for bidirectional P2P connections
	for connKey, status := range connectionStatus {
		if status != P2PAvailable {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		// Check for reverse direction
		reverseKey := toID + "->" + fromID
		reverseStatus, exists := connectionStatus[reverseKey]

		// If both directions are P2PAvailable and we haven't rendered either yet
		if exists && reverseStatus == P2PAvailable && !renderedConnections[connKey] && !renderedConnections[reverseKey] {
			edge, err := graph.CreateEdge("bidir_"+fromID+"_"+toID, nodes[fromID], nodes[toID])
			if err != nil {
				return nil, nil, err
			}

			edge.SetDir(cgraph.BothDir)
			edge.SetColor("green")
			edge.SetPenWidth(2.0)
			edge.SetLabel("Full P2P")

			renderedConnections[connKey] = true
			renderedConnections[reverseKey] = true
		}
	}

	// Second pass: Handle one-way P2P connections
	for connKey, status := range connectionStatus {
		if status != P2PAvailable || renderedConnections[connKey] {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		edge, err := graph.CreateEdge("p2p_"+fromID+"_"+toID, nodes[fromID], nodes[toID])
		if err != nil {
			return nil, nil, err
		}

		edge.SetColor("blue")
		edge.SetPenWidth(1.5)
		edge.SetLabel("P2P")

		renderedConnections[connKey] = true
	}

	// Third pass: Handle connections through the supernode
	// Track supernode connections to avoid duplicates
	peerToSuper := make(map[string]bool)
	superToPeer := make(map[string]bool)

	for connKey, status := range connectionStatus {
		if status == P2PAvailable || renderedConnections[connKey] {
			continue
		}

		parts := strings.Split(connKey, "->")
		fromID := parts[0]
		toID := parts[1]

		// Connection from peer to supernode
		if !peerToSuper[fromID] {
			edge, err := graph.CreateEdge("to_super_"+fromID, nodes[fromID], nodes[supernodeName])
			if err != nil {
				return nil, nil, err
			}

			edge.SetColor("red")
			edge.SetStyle(cgraph.DashedEdgeStyle)

			peerToSuper[fromID] = true
		}

		// Connection from supernode to peer
		if !superToPeer[toID] {
			edge, err := graph.CreateEdge("from_super_"+toID, nodes[supernodeName], nodes[toID])
			if err != nil {
				return nil, nil, err
			}

			edge.SetColor("red")
			edge.SetStyle(cgraph.DashedEdgeStyle)

			superToPeer[toID] = true
		}
	}

	return g, graph, nil
}

// RenderP2PGraph renders the graph to a file
func RenderP2PGraph(peerInfos []PeerP2PInfos, supernodeName, outputPath string) error {
	g, graph, err := GenerateP2PGraph(peerInfos, supernodeName)
	if err != nil {
		return err
	}

	defer func() {
		if err := graph.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		g.Close()
	}()

	// Determine format based on file extension
	format := graphviz.PNG // default
	if strings.HasSuffix(outputPath, ".svg") {
		format = graphviz.SVG
	} else if strings.HasSuffix(outputPath, ".dot") {
		format = graphviz.XDOT
	}

	// Render to file
	return g.RenderFilename(graph, format, outputPath)
}

// GetDOTFile returns the DOT representation as a string
func GetDOTFile(peerInfos []PeerP2PInfos, supernodeName string) (string, error) {
	g, graph, err := GenerateP2PGraph(peerInfos, supernodeName)
	if err != nil {
		return "", err
	}

	defer func() {
		if err := graph.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		g.Close()
	}()

	// Get DOT representation
	var buf strings.Builder
	if err := g.Render(graph, graphviz.DOT, &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Example of how to use this function
func main() {
	// Example data setup
	peer1 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.1"),
			Desc:      "Peer 1",
		},
	}

	peer2 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.2"),
			Desc:      "Peer 2",
		},
		P2PStatus: P2PAvailable,
	}

	peer3 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.3"),
			Desc:      "Peer 3",
		},
		P2PStatus: P2PPending,
	}

	// Create peer info collection
	peerInfos := []PeerP2PInfos{
		{
			From: peer1,
			To: []*Peer{
				peer2,
				peer3,
			},
		},
		{
			From: peer2,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PUnavailable,
				},
			},
		},
		{
			From: peer3,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
	}

	// Option 1: Get the DOT representation as a string
	dotContent, err := GetDOTFile(peerInfos, "SuperNode")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating DOT: %v\n", err)
		return
	}

	fmt.Println("Generated DOT file:")
	fmt.Println(dotContent)

	// Option 2: Render directly to a file (uncomment to use)
	/*
		outputFile := "p2p_network.png" // Can also use .svg or .dot
		err = RenderP2PGraph(peerInfos, "SuperNode", outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering graph: %v\n", err)
			return
		}
		fmt.Printf("Graph rendered to %s\n", outputFile)
	//
}
*/
/*
package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Type definitions from your code
type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

func (pt P2PCapacity) String() string {
	switch pt {
	case P2PUnknown:
		return "Unknown"
	case P2PPending:
		return "Pending"
	case P2PAvailable:
		return "Available"
	case P2PUnavailable:
		return "Unavailable"
	default:
		return "Unknown"
	}
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	Community string           `json:"community"`
	Desc      string           `json:"desc"`
}

type Peer struct {
	Infos      PeerInfo
	P2PStatus  P2PCapacity
	P2PCheckID string
	pendingTTL int
	UpdatedAt  time.Time
}

type PeerP2PInfos struct {
	From *Peer
	To   []*Peer
}

// GenerateP2PGraphviz creates a Graphviz DOT file representation of peer-to-peer connections
func GenerateP2PGraphviz(peerInfos []PeerP2PInfos, supernodeName string, layout string) (string, error) {
	// Create a new directed graph
	var g strings.Builder
	g.WriteString("digraph P2PNetwork {\n")

	// Graph settings
	g.WriteString("  // Graph settings\n")
	if layout == "circo" {
		g.WriteString("  layout=circo;\n")
		g.WriteString(fmt.Sprintf("  \"%s\" [root=true, pin=true, pos=\"0,0!\"];\n", supernodeName))
	} else {
		g.WriteString("  rankdir=LR;\n") // Default left to right layout
	}

	g.WriteString("  node [shape=box, style=\"rounded,filled\", fillcolor=lightblue];\n")
	g.WriteString("  overlap=false;\n")
	g.WriteString("  splines=true;\n")

	// Legend
	g.WriteString("  // Legend\n")
	g.WriteString("  subgraph cluster_legend {\n")
	g.WriteString("    label=\"Legend\";\n")
	g.WriteString("    style=filled;\n")
	g.WriteString("    color=lightgrey;\n")
	g.WriteString("    \"P2P Direct\" [shape=plaintext, label=\"→ Direct P2P connection\"];\n")
	g.WriteString("    \"Via Supernode\" [shape=plaintext, label=\"- - → Via Supernode (with destination peer)\"];\n")
	g.WriteString("    \"Bidirectional P2P\" [shape=plaintext, label=\"⟷ Full P2P (both directions)\"];\n")
	g.WriteString("  }\n\n")

	// Add supernode
	g.WriteString("  // Supernode\n")
	g.WriteString(fmt.Sprintf("  \"%s\" [shape=ellipse, fillcolor=lightgreen];\n", supernodeName))
	g.WriteString("\n")

	// Map to keep track of added nodes (to avoid duplicates)
	addedNodes := make(map[string]bool)

	// Add all peers as nodes
	g.WriteString("  // Peer nodes\n")
	for _, peerInfo := range peerInfos {
		fromPeer := peerInfo.From
		fromID := fromPeer.Infos.VirtualIP.String()

		if !addedNodes[fromID] {
			g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
				fromID,
				fromID,
				fromPeer.Infos.Desc))
			addedNodes[fromID] = true
		}

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()

			if !addedNodes[toID] {
				g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
					toID,
					toID,
					toPeer.Infos.Desc))
				addedNodes[toID] = true
			}
		}
	}
	g.WriteString("\n")

	// Create a map to store connection status between pairs of peers
	type ConnectionInfo struct {
		Status   P2PCapacity
		FromPeer string
		ToPeer   string
	}

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

	// Track rendered connections to avoid duplicates
	renderedConnections := make(map[string]bool)

	// First pass: Check for bidirectional P2P connections
	g.WriteString("  // Bidirectional P2P connections\n")
	for connKey, connInfo := range connectionData {
		if connInfo.Status != P2PAvailable {
			continue
		}

		fromID := connInfo.FromPeer
		toID := connInfo.ToPeer

		// Check for reverse direction
		reverseKey := toID + "->" + fromID
		reverseInfo, exists := connectionData[reverseKey]

		// If both directions are P2PAvailable and we haven't rendered either yet
		if exists && reverseInfo.Status == P2PAvailable && !renderedConnections[connKey] && !renderedConnections[reverseKey] {
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [dir=both, color=green, penwidth=2.0, label=\"Full P2P\"];\n",
				fromID, toID))
			renderedConnections[connKey] = true
			renderedConnections[reverseKey] = true
		}
	}
	g.WriteString("\n")

	// Second pass: Handle one-way P2P connections
	g.WriteString("  // One-way P2P connections\n")
	for connKey, connInfo := range connectionData {
		if connInfo.Status != P2PAvailable || renderedConnections[connKey] {
			continue
		}

		fromID := connInfo.FromPeer
		toID := connInfo.ToPeer

		g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=blue, penwidth=1.5, label=\"P2P\"];\n",
			fromID, toID))
		renderedConnections[connKey] = true
	}
	g.WriteString("\n")

	// Third pass: Handle connections through the supernode with clear indicators
	g.WriteString("  // Connections through supernode\n")

	// Create a color map for peers
	peerColors := make(map[string]string)
	colorOptions := []string{"darkred", "darkorange", "purple", "brown", "darkgreen", "navy", "darkslategray"}
	colorIndex := 0

	// Assign a color to each peer
	for nodeID := range addedNodes {
		peerColors[nodeID] = colorOptions[colorIndex%len(colorOptions)]
		colorIndex++
	}

	// Create edges for supernode-routed connections
	for connKey, connInfo := range connectionData {
		if connInfo.Status == P2PAvailable || renderedConnections[connKey] {
			continue
		}

		fromID := connInfo.FromPeer
		toID := connInfo.ToPeer

		// Use the peer's assigned color
		peerColor := peerColors[fromID]

		// Connection from source peer to supernode (shows target in label)
		g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, "+
			"label=\"To %s\", fontcolor=\"%s\"];\n",
			fromID, supernodeName, peerColor, toID, peerColor))

		// Connection from supernode to target peer (shows source in label)
		g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, "+
			"label=\"From %s\", fontcolor=\"%s\"];\n",
			supernodeName, toID, peerColor, fromID, peerColor))

		renderedConnections[connKey] = true
	}

	g.WriteString("}\n")
	return g.String(), nil
}

// SaveP2PGraphvizToFile saves the generated DOT representation to a file
func SaveP2PGraphvizToFile(peerInfos []PeerP2PInfos, supernodeName, layout, filePath string) error {
	dotContent, err := GenerateP2PGraphviz(peerInfos, supernodeName, layout)
	if err != nil {
		return err
	}

	// Write to file (requires "os" import)
	// return os.WriteFile(filePath, []byte(dotContent), 0644)

	// This is a placeholder until you import "os"
	fmt.Println("Generated DOT file:")
	fmt.Println(dotContent)
	return nil
}

// Example of how to use this function
func main() {
	// Example data setup
	peer1 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.1"),
			Desc:      "Peer 1",
		},
	}

	peer2 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.2"),
			Desc:      "Peer 2",
		},
		P2PStatus: P2PAvailable,
	}

	peer3 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.3"),
			Desc:      "Peer 3",
		},
		P2PStatus: P2PPending,
	}

	peer4 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.4"),
			Desc:      "Peer 4",
		},
		P2PStatus: P2PUnavailable,
	}

	// Create peer info collection
	peerInfos := []PeerP2PInfos{
		{
			From: peer1,
			To: []*Peer{
				peer2,
				peer3,
				peer4,
			},
		},
		{
			From: peer2,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PUnavailable,
				},
				{
					Infos:     peer4.Infos,
					P2PStatus: P2PPending,
				},
			},
		},
		{
			From: peer3,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer4.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
		{
			From: peer4,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PPending,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
	}

	// Generate the DOT file with circo layout
	SaveP2PGraphvizToFile(peerInfos, "SuperNode", "circo", "p2p_network.dot")

	// Alternatively, use default layout
	// SaveP2PGraphvizToFile(peerInfos, "SuperNode", "", "p2p_network.dot")
}
*/

package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Type definitions from your code
type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

func (pt P2PCapacity) String() string {
	switch pt {
	case P2PUnknown:
		return "Unknown"
	case P2PPending:
		return "Pending"
	case P2PAvailable:
		return "Available"
	case P2PUnavailable:
		return "Unavailable"
	default:
		return "Unknown"
	}
}

type PeerInfo struct {
	VirtualIP netip.Addr       `json:"virtualIP"`
	MACAddr   net.HardwareAddr `json:"macAddr"`
	PubSocket *net.UDPAddr     `json:"pubSocket"`
	Community string           `json:"community"`
	Desc      string           `json:"desc"`
}

type Peer struct {
	Infos      PeerInfo
	P2PStatus  P2PCapacity
	P2PCheckID string
	pendingTTL int
	UpdatedAt  time.Time
}

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

// GenerateP2PGraphviz creates a Graphviz DOT file representation of peer-to-peer connections
func GenerateP2PGraphviz(peerInfos []PeerP2PInfos, supernodeName string, layout string) (string, error) {
	// Create a new directed graph
	var g strings.Builder
	g.WriteString("digraph P2PNetwork {\n")

	// Graph settings
	g.WriteString("  // Graph settings\n")
	if layout == "circo" {
		g.WriteString("  layout=circo;\n")
		g.WriteString(fmt.Sprintf("  \"%s\" [root=true, pin=true, pos=\"0,0!\"];\n", supernodeName))
	} else {
		g.WriteString("  rankdir=LR;\n") // Default left to right layout
	}

	g.WriteString("  node [shape=box, style=\"rounded,filled\", fillcolor=lightblue];\n")
	g.WriteString("  overlap=false;\n")
	g.WriteString("  splines=true;\n")

	// Legend
	g.WriteString("  // Legend\n")
	g.WriteString("  subgraph cluster_legend {\n")
	g.WriteString("    label=\"Legend\";\n")
	g.WriteString("    style=filled;\n")
	g.WriteString("    color=lightgrey;\n")
	g.WriteString("    \"FullP2P\" [shape=plaintext, label=\"⟷ Full P2P (both directions)\"];\n")
	g.WriteString("    \"PartialP2P\" [shape=plaintext, label=\"→ Partial P2P (one direction only)\"];\n")
	g.WriteString("    \"SuperRelay\" [shape=plaintext, label=\"- - → Via Supernode relay\"];\n")
	g.WriteString("  }\n\n")

	// Add supernode
	g.WriteString("  // Supernode\n")
	g.WriteString(fmt.Sprintf("  \"%s\" [shape=ellipse, fillcolor=lightgreen];\n", supernodeName))
	g.WriteString("\n")

	// Map to keep track of added nodes (to avoid duplicates)
	addedNodes := make(map[string]bool)

	// Add all peers as nodes
	g.WriteString("  // Peer nodes\n")
	for _, peerInfo := range peerInfos {
		fromPeer := peerInfo.From
		fromID := fromPeer.Infos.VirtualIP.String()

		if !addedNodes[fromID] {
			g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
				fromID,
				fromID,
				fromPeer.Infos.Desc))
			addedNodes[fromID] = true
		}

		for _, toPeer := range peerInfo.To {
			toID := toPeer.Infos.VirtualIP.String()

			if !addedNodes[toID] {
				g.WriteString(fmt.Sprintf("  \"%s\" [label=\"%s\\n%s\"];\n",
					toID,
					toID,
					toPeer.Infos.Desc))
				addedNodes[toID] = true
			}
		}
	}
	g.WriteString("\n")

	// Build a map of peer connection status
	type ConnectionInfo struct {
		Status   P2PCapacity
		FromPeer string
		ToPeer   string
	}

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

	// Create a color map for peer pairs
	peerColors := make(map[string]string)
	colorOptions := []string{"darkred", "darkorange", "purple", "brown", "darkgreen", "navy", "darkslategray", "chocolate", "indigo"}
	colorIndex := 0

	// Get the connection type between two peers
	getConnectionType := func(peerA, peerB string) ConnectionType {
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

	// Process each peer pair and render the appropriate connections
	g.WriteString("  // Peer connections\n")
	for pairKey := range peerPairs {
		peers := strings.Split(pairKey, ",")
		peerA := peers[0]
		peerB := peers[1]

		// Assign a unique color for this peer pair
		if _, exists := peerColors[pairKey]; !exists {
			peerColors[pairKey] = colorOptions[colorIndex%len(colorOptions)]
			colorIndex++
		}
		peerColor := peerColors[pairKey]

		// Determine the connection type
		connType := getConnectionType(peerA, peerB)

		switch connType {
		case FullP2P:
			// Full P2P: One bidirectional edge
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [dir=both, color=\"%s\", penwidth=2.0, label=\"Full P2P\"];\n",
				peerA, peerB, peerColor))

		case PartialP2P_AtoB:
			// Partial P2P (A->B direct, B->A via supernode)
			// Direct connection A->B
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", penwidth=1.5];\n",
				peerA, peerB, peerColor))

			// B to supernode (going to A)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"to %s\"];\n",
				peerB, supernodeName, peerColor, peerColor, peerA))

			// Supernode to A (coming from B)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"from %s\"];\n",
				supernodeName, peerA, peerColor, peerColor, peerB))

		case PartialP2P_BtoA:
			// Partial P2P (B->A direct, A->B via supernode)
			// Direct connection B->A
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", penwidth=1.5];\n",
				peerB, peerA, peerColor))

			// A to supernode (going to B)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"to %s\"];\n",
				peerA, supernodeName, peerColor, peerColor, peerB))

			// Supernode to B (coming from A)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"from %s\"];\n",
				supernodeName, peerB, peerColor, peerColor, peerA))

		case NoP2P:
			// No P2P: Both directions via supernode
			// Supernode to B (coming from A)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"from %s\"];\n",
				supernodeName, peerB, peerColor, peerColor, peerA))

			// Supernode to A (coming from B)
			g.WriteString(fmt.Sprintf("  \"%s\" -> \"%s\" [color=\"%s\", style=dashed, fontcolor=\"%s\", label=\"from %s\"];\n",
				supernodeName, peerA, peerColor, peerColor, peerB))
		}
	}

	g.WriteString("}\n")
	return g.String(), nil
}

// SaveP2PGraphvizToFile saves the generated DOT representation to a file
func SaveP2PGraphvizToFile(peerInfos []PeerP2PInfos, supernodeName, layout, filePath string) error {
	dotContent, err := GenerateP2PGraphviz(peerInfos, supernodeName, layout)
	if err != nil {
		return err
	}

	// Write to file (requires "os" import)
	// return os.WriteFile(filePath, []byte(dotContent), 0644)

	// This is a placeholder until you import "os"
	fmt.Println("Generated DOT file:")
	fmt.Println(dotContent)
	return nil
}

// Example of how to use this function
func main() {
	// Example data setup
	peer1 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.1"),
			Desc:      "Peer 1",
		},
	}

	peer2 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.2"),
			Desc:      "Peer 2",
		},
		P2PStatus: P2PAvailable,
	}

	peer3 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.3"),
			Desc:      "Peer 3",
		},
		P2PStatus: P2PPending,
	}

	peer4 := &Peer{
		Infos: PeerInfo{
			VirtualIP: netip.MustParseAddr("10.0.0.4"),
			Desc:      "Peer 4",
		},
		P2PStatus: P2PUnavailable,
	}

	// Create peer info collection
	peerInfos := []PeerP2PInfos{
		{
			From: peer1,
			To: []*Peer{
				peer2,
				peer3,
				peer4,
			},
		},
		{
			From: peer2,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PUnavailable,
				},
				{
					Infos:     peer4.Infos,
					P2PStatus: P2PPending,
				},
			},
		},
		{
			From: peer3,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PAvailable,
				},
				{
					Infos:     peer4.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
		{
			From: peer4,
			To: []*Peer{
				{
					Infos:     peer1.Infos,
					P2PStatus: P2PPending,
				},
				{
					Infos:     peer2.Infos,
					P2PStatus: P2PUnknown,
				},
				{
					Infos:     peer3.Infos,
					P2PStatus: P2PAvailable,
				},
			},
		},
	}

	// Generate the DOT file with circo layout
	SaveP2PGraphvizToFile(peerInfos, "SuperNode", "circo", "p2p_network.dot")

	// Alternatively, use default layout
	// SaveP2PGraphvizToFile(peerInfos, "SuperNode", "", "p2p_network.dot")
}

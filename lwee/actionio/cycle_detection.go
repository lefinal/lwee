package actionio

import (
	"github.com/lefinal/meh"
	"strings"
)

func assureNoCycles(forwarders []*sourceForwarder) error {
	ioGraph := newTarjanGraph()
	entityIDsByName := make(map[string]node)
	// Register all edges.
	for _, forwarder := range forwarders {
		// Get provider id.
		if forwarder.writer.entityName == "" {
			return meh.NewInternalErr("missing provider entity name", meh.Details{"provider": forwarder.writer.providerName})
		}
		if len(forwarder.readers) == 0 {
			continue
		}
		providerID, ok := entityIDsByName[forwarder.writer.entityName]
		if !ok {
			providerID = node(len(entityIDsByName))
			entityIDsByName[forwarder.writer.entityName] = providerID
		}
		for _, reader := range forwarder.readers {
			// Get requester id.
			if reader.sourceEntityName == "" {
				return meh.NewInternalErr("missing requester entity name", meh.Details{"requester": reader.requesterName})
			}
			requesterID, ok := entityIDsByName[reader.sourceEntityName]
			if !ok {
				requesterID = node(len(entityIDsByName))
				entityIDsByName[reader.sourceEntityName] = requesterID
			}
			// Register edge in graph.
			ioGraph.addEdge(providerID, requesterID)
		}
	}
	// Detect cycles.
	detectedCycles := ioGraph.detectCycles()
	if len(detectedCycles) == 0 {
		return nil
	}
	// Cycles detected. Create a proper error message with details about the cycle.
	var errMessage strings.Builder
	errMessage.WriteString("detected cycle(s) in action io: ")
	for cycleNum, involvedNodes := range detectedCycles {
		for i := len(involvedNodes) - 1; i >= 0; i-- {
			// Look up entity name.
			involvedNodeName := ""
			for entityName, entityID := range entityIDsByName {
				if entityID == involvedNodes[i] {
					involvedNodeName = entityName
					break
				}
			}
			// Add to the error message.
			errMessage.WriteString(involvedNodeName)
			if i > 0 {
				errMessage.WriteString(" -> ")
			}
		}
		if cycleNum > 0 {
			errMessage.WriteString("; ")
		}
	}

	return meh.NewBadInputErr(errMessage.String(), nil)
}

type node int

type tarjanGraph struct {
	// nodeConnections represents the adjacency list of the graph.
	nodeConnections map[node]map[node]struct{}
	// S is a stack used to hold nodes. When traversing a node, we push the node onto
	// the stack. When we have finished processing a strongly connected component, we
	// remove it from the stack.
	S []node
	// indexByNode holds the "depth" or "indexByNode" values for each node, it
	// represents at which depth or step in the DFS exploration each node was
	// encountered. It is used to distinguish nodes which are visited and not yet
	// visited.
	indexByNode map[node]int
	// lowLinkByNode holds the smallest "depth" or "index" reachable from each node.
	// It is used to identify whether a node is part of a cycle - or more generally,
	// a strongly connected component. Another explanation of the low-link value is
	// the smallest index of any node known to be reachable from v, including v
	// itself.
	lowLinkByNode map[node]int
	// newEncounterIndex is a counter to assign unique "depth" or "index" values to
	// each node as it is first encountered in the DFS traversal.
	newEncounterIndex int
	// stronglyConnectedComponents partition the graph into regions within which
	// every pair of nodes is mutually reachable, representing potential cycles in
	// the graph.
	stronglyConnectedComponents [][]node
}

func newTarjanGraph() *tarjanGraph {
	return &tarjanGraph{
		nodeConnections:             make(map[node]map[node]struct{}),
		S:                           make([]node, 0),
		indexByNode:                 make(map[node]int, 0),
		lowLinkByNode:               make(map[node]int, 0),
		newEncounterIndex:           0,
		stronglyConnectedComponents: make([][]node, 0),
	}
}

func (g *tarjanGraph) addEdge(n1 node, n2 node) {
	if _, exists := g.nodeConnections[n1]; !exists {
		g.nodeConnections[n1] = make(map[node]struct{})
	}
	g.nodeConnections[n1][n2] = struct{}{}
}

// strongConnect contributes to identifying strongly connected components in the
// graph.
func (g *tarjanGraph) strongConnect(current node) {
	// Assign the smallest unused index to node.
	g.indexByNode[current] = g.newEncounterIndex
	g.lowLinkByNode[current] = g.newEncounterIndex
	g.newEncounterIndex++
	// Push to stack.
	g.S = append(g.S, current)

	// Check all outgoing connections from this node.
	for follower := range g.nodeConnections[current] {
		// Check if already encountered.
		if _, ok := g.indexByNode[follower]; !ok {
			// Not encountered yet. Check also for the follower and update the low-link
			// value.
			g.strongConnect(follower)
			g.lowLinkByNode[current] = min(g.lowLinkByNode[current], g.lowLinkByNode[follower])
		} else {
			onStack := false
			for _, nodeOnStack := range g.S {
				if nodeOnStack == follower {
					onStack = true
					break
				}
			}
			if onStack {
				g.lowLinkByNode[current] = min(g.lowLinkByNode[current], g.indexByNode[follower])
			}
		}
	}

	if g.lowLinkByNode[current] == g.indexByNode[current] {
		stronglyConnectedComponent := make([]node, 0)
		for {
			w := g.S[len(g.S)-1]
			g.S = g.S[:len(g.S)-1]
			stronglyConnectedComponent = append(stronglyConnectedComponent, w)
			if w == current {
				break
			}
		}
		g.stronglyConnectedComponents = append(g.stronglyConnectedComponents, stronglyConnectedComponent)
	}
}

// detectCycles returns all detected cycles with their all involved nodes. Don't
// call this function multiple times.
func (g *tarjanGraph) detectCycles() [][]node {
	for v := range g.nodeConnections {
		if _, exists := g.indexByNode[v]; !exists {
			g.strongConnect(v)
		}
	}
	detectedCycles := make([][]node, 0)
	for _, stronglyConnectedComponent := range g.stronglyConnectedComponents {
		if len(stronglyConnectedComponent) > 1 {
			detectedCycles = append(detectedCycles, stronglyConnectedComponent)
		}
	}
	return detectedCycles
}

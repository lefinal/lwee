package actionio

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

// tarjanSuite tests tarjanGraph.detectCycles.
type tarjanSuite struct {
	suite.Suite
	g *tarjanGraph
}

func (suite *tarjanSuite) SetupTest() {
	suite.g = newTarjanGraph()
}

func (suite *tarjanSuite) connect(nodes ...node) {
	for i := 0; i < len(nodes)-1; i++ {
		suite.g.addEdge(nodes[i], nodes[i+1])
	}
}

func (suite *tarjanSuite) TestEmpty() {
	got := suite.g.detectCycles()
	suite.Empty(got)
}

func (suite *tarjanSuite) TestSingleNode() {
	suite.g.addEdge(1, 1)
	got := suite.g.detectCycles()
	suite.Empty(got)
}

func (suite *tarjanSuite) TestIsolatedNodes() {
	suite.g.addEdge(1, 1)
	suite.g.addEdge(2, 2)
	suite.g.addEdge(3, 3)

	got := suite.g.detectCycles()
	suite.Empty(got)
}

func (suite *tarjanSuite) TestChain() {
	suite.connect(1, 2, 3, 6, 7)

	got := suite.g.detectCycles()
	suite.Empty(got)
}

func (suite *tarjanSuite) TestChainWithIsolatedNodes() {
	suite.connect(1, 2, 5, 7)
	suite.g.addEdge(3, 3)
	suite.g.addEdge(4, 4)
	suite.connect(10, 11, 12)

	got := suite.g.detectCycles()
	suite.Empty(got)
}

func (suite *tarjanSuite) TestCycle1() {
	suite.connect(1, 2, 1)

	got := suite.g.detectCycles()
	suite.Require().Len(got, 1)
	suite.Len(got[0], 2)
	suite.Contains(got[0], node(1))
	suite.Contains(got[0], node(2))
}

func (suite *tarjanSuite) TestCycle2() {
	suite.connect(1, 2, 3, 4)
	suite.g.addEdge(4, 2)
	suite.g.addEdge(4, 3)
	suite.g.addEdge(4, 5)

	got := suite.g.detectCycles()
	suite.Require().Len(got, 1)
	suite.Len(got[0], 3)
	suite.Contains(got[0], node(2))
	suite.Contains(got[0], node(3))
	suite.Contains(got[0], node(4))
}

func (suite *tarjanSuite) TestCycle3() {
	suite.connect(1, 2, 3, 4, 5, 6, 7, 8)
	suite.connect(3, 6)
	suite.connect(4, 2)
	suite.connect(8, 3)

	got := suite.g.detectCycles()
	suite.Len(got, 1)
}

func (suite *tarjanSuite) TestCycle4() {
	suite.connect(1, 2, 3, 4, 5, 6, 7, 8)
	suite.connect(3, 6)
	suite.connect(4, 2)
	suite.connect(8, 6)

	got := suite.g.detectCycles()
	suite.Len(got, 2)
}

func Test_tarjan(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(tarjanSuite))
}

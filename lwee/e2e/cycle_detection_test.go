package e2e

import (
	"github.com/stretchr/testify/suite"
	"testing"
)

type CycleDetectionSuite struct {
	suite.Suite
	e2eConfig config
}

func (suite *CycleDetectionSuite) SetupTest() {
	suite.e2eConfig = config{
		command:      "run",
		contextDir:   "./cycle-detection/project",
		flowFilename: "flow-non-cyclic.yaml",
	}
}

func (suite *CycleDetectionSuite) TestCycle() {
	suite.e2eConfig.flowFilename = "flow-cyclic.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().Error(err, "should fail")
	suite.ErrorContains(err, "cycle")
}

func (suite *CycleDetectionSuite) TestNoCycle() {
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func TestCycleDetection(t *testing.T) {
	suite.Run(t, new(CycleDetectionSuite))
}

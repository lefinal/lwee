package e2e

import (
	"github.com/stretchr/testify/suite"
	"runtime"
	"testing"
)

type CommandAssertSuite struct {
	suite.Suite
	e2eConfig config
}

func (suite *CommandAssertSuite) SetupTest() {
	// Skip on windows as we use "/bin/bash".
	if runtime.GOOS == "windows" {
		suite.T().Skip("test skipped on windows due to no /bin/bash being available")
		return
	}
	suite.e2eConfig = config{
		command:      "run",
		contextDir:   "./command-assert/project",
		flowFilename: "flow-no-assertions.yaml",
	}
}

func (suite *CommandAssertSuite) TestNoAssertions() {
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *CommandAssertSuite) TestShouldContainOK() {
	suite.e2eConfig.flowFilename = "flow-should-contain-ok.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *CommandAssertSuite) TestShouldContainFail() {
	suite.e2eConfig.flowFilename = "flow-should-contain-fail.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().Error(err, "should fail")
	suite.ErrorContains(err, "assert")
}

func (suite *CommandAssertSuite) TestShouldEqualOK() {
	suite.e2eConfig.flowFilename = "flow-should-equal-ok.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *CommandAssertSuite) TestShouldEqualFail() {
	suite.e2eConfig.flowFilename = "flow-should-equal-fail.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().Error(err, "should fail")
	suite.ErrorContains(err, "assert")
}

func (suite *CommandAssertSuite) TestShouldMatchRegexOK() {
	suite.e2eConfig.flowFilename = "flow-should-match-regex-ok.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *CommandAssertSuite) TestShouldMatchRegexFail() {
	suite.e2eConfig.flowFilename = "flow-should-match-regex-fail.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().Error(err, "should fail")
	suite.ErrorContains(err, "assert")
}

func TestCommandAssert(t *testing.T) {
	suite.Run(t, new(CommandAssertSuite))
}

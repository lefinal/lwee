package e2e

import (
	"github.com/stretchr/testify/suite"
	"os"
	"path"
	"testing"
)

type flowValidationSuite struct {
	suite.Suite
	e2eConfig config
}

func (suite *flowValidationSuite) SetupTest() {
	suite.e2eConfig = config{
		command:      "verify",
		contextDir:   "./flow-validation/project",
		flowFilename: "flow-valid.yaml",
	}
}

func (suite *flowValidationSuite) TearDownTest() {
	_ = os.RemoveAll(path.Join(suite.e2eConfig.contextDir, "out"))
}

func (suite *flowValidationSuite) TestOK() {
	suite.e2eConfig.flowFilename = "flow-valid.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *flowValidationSuite) TestInvalid1() {
	suite.e2eConfig.flowFilename = "flow-invalid.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Error(err, "should fail")
}

func (suite *flowValidationSuite) TestInvalid2() {
	suite.e2eConfig.flowFilename = "flow-invalid-2.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.Error(err, "should fail")
}

func (suite *flowValidationSuite) TestWarnings() {
	suite.e2eConfig.flowFilename = "flow-warnings.yaml"
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func TestFlowValidation(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(flowValidationSuite))
}

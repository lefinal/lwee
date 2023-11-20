package e2e

import (
	"github.com/stretchr/testify/suite"
	"os"
	"path"
	"testing"
)

// cliSuite tests CLI features like finding context directories, etc.
type cliSuite struct {
	suite.Suite
	originalWorkdir string
	e2eConfig       config
}

func (suite *cliSuite) SetupTest() {
	suite.e2eConfig = config{
		command:      "run",
		contextDir:   "./cli/project",
		flowFilename: "flow.yaml",
	}
	workdir, err := os.Getwd()
	suite.Require().NoError(err, "get workdir should not fail")
	suite.T().Cleanup(func() {
		suite.Require().NoError(os.Chdir(workdir))
	})
	suite.originalWorkdir = workdir
}

func (suite *cliSuite) TestNoFlowFilename() {
	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestNonAbsFlowFilename() {
	suite.e2eConfig.flowFilename = "flow.yaml"

	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestManualDir() {
	err := os.Chdir("/tmp")
	suite.Require().NoError(err, "cd to /tmp should not fail")

	suite.e2eConfig.contextDir = path.Join(suite.originalWorkdir, suite.e2eConfig.contextDir)

	err = run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestAbsFlowFilename() {
	// Copy flow file to another directory.
	newFlowFilename := path.Join(suite.T().TempDir(), "my-external-flow.yaml")
	srcFlowFileRaw, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, suite.e2eConfig.flowFilename))
	suite.Require().NoError(err, "read original flow file should not fail")
	err = os.WriteFile(newFlowFilename, srcFlowFileRaw, 0644)
	suite.Require().NoError(err, "write new flow file should not fail")

	suite.e2eConfig.flowFilename = newFlowFilename

	err = run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestNoFlowFilenameSet() {
	suite.e2eConfig.flowFilename = ""

	err := run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestUnknownFlowFilename() {
	suite.e2eConfig.flowFilename = "unknown.yaml"
	suite.e2eConfig.skipFileExistenceChecks = true

	err := run(suite.T(), suite.e2eConfig)
	suite.Error(err, "should fail")
}

func (suite *cliSuite) TestNoContextDirButIsInWorkingDir() {
	err := os.Chdir(suite.e2eConfig.contextDir)
	suite.Require().NoError(err, "cd to context dir should not fail")

	suite.e2eConfig.contextDir = ""
	suite.e2eConfig.skipFileExistenceChecks = true

	err = run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestNoContextDirButIsInParentDir() {
	err := os.Chdir(path.Join(suite.e2eConfig.contextDir, "actions"))
	suite.Require().NoError(err, "cd to actions dir in context dir should not fail")

	suite.e2eConfig.contextDir = ""
	suite.e2eConfig.skipFileExistenceChecks = true

	err = run(suite.T(), suite.e2eConfig)
	suite.NoError(err, "should not fail")
}

func (suite *cliSuite) TestNoContextDirButIsNotInParentDir() {
	err := os.Chdir(suite.T().TempDir())
	suite.Require().NoError(err, "cd to actions dir in context dir should not fail")

	suite.e2eConfig.contextDir = ""
	suite.e2eConfig.skipFileExistenceChecks = true

	err = run(suite.T(), suite.e2eConfig)
	suite.Error(err, "should fail")
}

func (suite *cliSuite) TestInitInWorkdir() {
	tempDir := suite.T().TempDir()
	err := os.Chdir(tempDir)
	suite.Require().NoError(err, "cd to actions dir in context dir should not fail")

	suite.e2eConfig.command = "init"
	suite.e2eConfig.contextDir = ""
	suite.e2eConfig.skipFileExistenceChecks = true

	err = run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "should not fail")

	suite.FileExists(path.Join(tempDir, "flow.yaml"))
	suite.DirExists(path.Join(tempDir, "actions"))
}

func (suite *cliSuite) TestInitInWorkdirWithManualFlowFilename() {
	tempDir := suite.T().TempDir()
	err := os.Chdir(tempDir)
	suite.Require().NoError(err, "cd to actions dir in context dir should not fail")

	suite.e2eConfig.command = "init"
	suite.e2eConfig.contextDir = ""
	suite.e2eConfig.flowFilename = "my-special-flow.yaml"
	suite.e2eConfig.skipFileExistenceChecks = true

	err = run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "should not fail")

	suite.FileExists(path.Join(tempDir, suite.e2eConfig.flowFilename))
	suite.DirExists(path.Join(tempDir, "actions"))
}

func (suite *cliSuite) TestInitInManualDir() {
	suite.e2eConfig.command = "init"
	suite.e2eConfig.contextDir = suite.T().TempDir()
	suite.e2eConfig.flowFilename = ""
	suite.e2eConfig.skipFileExistenceChecks = true

	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "should not fail")

	suite.FileExists(path.Join(suite.e2eConfig.contextDir, "flow.yaml"))
	suite.DirExists(path.Join(suite.e2eConfig.contextDir, "actions"))
}

func (suite *cliSuite) TestInitInManualDirWithManualFlowFilename() {
	suite.e2eConfig.command = "init"
	suite.e2eConfig.contextDir = suite.T().TempDir()
	suite.e2eConfig.flowFilename = "my-special-flow.yaml"
	suite.e2eConfig.skipFileExistenceChecks = true

	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "should not fail")

	suite.FileExists(path.Join(suite.e2eConfig.contextDir, suite.e2eConfig.flowFilename))
	suite.DirExists(path.Join(suite.e2eConfig.contextDir, "actions"))
}

func TestCLI(t *testing.T) {
	// Do not run tests in parallel as they involve changing the working directory.
	suite.Run(t, new(cliSuite))
}

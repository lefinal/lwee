package locator

import (
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"os"
	"path"
	"testing"
)

const flowFilename = "flow.yaml"

// FindContextDirSuite tests FindContextDir.
type FindContextDirSuite struct {
	suite.Suite
	startDir string
}

func (suite *FindContextDirSuite) SetupTest() {
	suite.startDir = suite.T().TempDir()
}

func (suite *FindContextDirSuite) mkdirAll(dir string) {
	suite.Require().NoError(os.MkdirAll(dir, mkdirPerm), "mkdir all should not fail")
}

func (suite *FindContextDirSuite) initProject(dir string) {
	l, err := New(dir, path.Join(dir, flowFilename))
	suite.Require().NoError(err, "new locator should not fail")
	err = l.InitProject(zap.NewNop())
	suite.Require().NoError(err, "init project should not fail")
}

func (suite *FindContextDirSuite) TestNotFound1() {
	_, err := FindContextDir(suite.startDir)
	suite.Error(err, "should fail")
}

func (suite *FindContextDirSuite) TestStartDirIsProject() {
	suite.initProject(suite.startDir)

	contextDir, err := FindContextDir(suite.startDir)
	suite.NoError(err, "should not fail")
	suite.Equal(suite.startDir, contextDir, "should return correct context dir")
}

func (suite *FindContextDirSuite) TestChildDirIsProject() {
	dir := path.Join(suite.startDir, "my-sub-dir")
	suite.mkdirAll(dir)
	suite.initProject(dir)

	contextDir, err := FindContextDir(dir)
	suite.NoError(err, "should not fail")
	suite.Equal(dir, contextDir, "should return correct context dir")
}

func (suite *FindContextDirSuite) TestParentDirIsProject() {
	dir := path.Join(suite.startDir, "my-sub-dir")
	suite.mkdirAll(dir)
	suite.initProject(suite.startDir)

	contextDir, err := FindContextDir(dir)
	suite.NoError(err, "should not fail")
	suite.Equal(suite.startDir, contextDir, "should return correct context dir")
}

func (suite *FindContextDirSuite) TestParentParentDirIsProject() {
	dir := path.Join(suite.startDir, "my-sub-dir", "my-sub-sub")
	suite.mkdirAll(dir)
	suite.initProject(suite.startDir)

	contextDir, err := FindContextDir(dir)
	suite.NoError(err, "should not fail")
	suite.Equal(suite.startDir, contextDir, "should return correct context dir")
}

func TestFindContextDir(t *testing.T) {
	suite.Run(t, new(FindContextDirSuite))
}

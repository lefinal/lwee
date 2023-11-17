package e2e

import (
	"context"
	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/lefinal/zaprec"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"io"
	"os"
	"path"
	"strings"
	"testing"
)

const logMessageForAppliedMoveFileOptimization = "filename optimization available. moving file."
const logMessageForFailedAppliedMoveFileOptimization = "copy source regularly due to failed move"
const logMessageForSkippedTransmission = "source reader skipped transmission"

type featuresSuite struct {
	suite.Suite
	e2eConfig config
}

func (suite *featuresSuite) SetupTest() {
	suite.e2eConfig = config{
		command:    "run",
		contextDir: "./features/project",
	}
}

func (suite *featuresSuite) TearDownTest() {
	_ = os.RemoveAll(path.Join(suite.e2eConfig.contextDir, "out"))
}

func (suite *featuresSuite) TestEnvWithProjectAction() {
	const expect = "Hello World from Project Action Runner!\n"

	suite.e2eConfig.flowFilename = path.Join(suite.e2eConfig.contextDir, "flow-env-proj-action.yaml")
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "run should not fail")

	// Check file.
	got, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, "out", "result.txt"))
	suite.Require().NoError(err, "read result file should not fail")
	suite.Equal(expect, string(got), "should have written correct result")
}

func (suite *featuresSuite) TestEnvWithImageRunner() {
	const imageTag = "lwee_e2e_print-env"
	const expect = "Hello World from Image Runner!"

	// Build image.
	suite.T().Log("build image")
	tar, err := archive.TarWithOptions("./features", &archive.TarOptions{
		IncludeFiles: []string{"print-env.Dockerfile"},
	})
	suite.Require().NoError(err, "tar image build context should not fail")
	dockerClient, err := dockerclient.NewClientWithOpts()
	suite.Require().NoError(err, "new docker client should not fail")
	response, err := dockerClient.ImageBuild(context.Background(), tar, types.ImageBuildOptions{
		Tags:       []string{"lwee_e2e_print-env"},
		Dockerfile: "print-env.Dockerfile",
	})
	suite.Require().NoError(err, "build image should not fail")
	_, _ = io.Copy(io.Discard, response.Body) // Must be read to complete the build.
	_ = response.Body.Close()
	suite.T().Logf("image %s successfully built", imageTag)
	suite.T().Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), imageTag, types.ImageRemoveOptions{})
	})

	// Run.
	suite.e2eConfig.flowFilename = path.Join(suite.e2eConfig.contextDir, "flow-env-image.yaml")
	err = run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "run should not fail")

	// Check file.
	got, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, "out", "result.txt"))
	suite.Require().NoError(err, "read result file should not fail")
	suite.Contains(string(got), expect, "should have written correct result")
}

func (suite *featuresSuite) TestFileCopyOptimizationFlowOutputFile() {
	const expectedLines = 32

	logger, logRecords := zaprec.NewRecorder(zap.DebugLevel)
	suite.e2eConfig.logger = logger

	// Run.
	suite.e2eConfig.flowFilename = path.Join(suite.e2eConfig.contextDir, "flow-file-copy-optimization-flow-out-file.yaml")
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "run should not fail")

	// Assure the output file is not empty.
	resultRaw, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, "out", "result.txt"))
	suite.Require().NoError(err, "read output file should not fail")
	suite.Len(strings.Split(string(resultRaw), "\n"), expectedLines+1, "should have written correct lines")

	// Assure that the optimization has been applied.
	logEntryForSkippedTransmission := false
	logEntryForAppliedMove := false
	for _, record := range logRecords.Records() {
		if strings.Contains(record.Entry.Message, logMessageForSkippedTransmission) {
			logEntryForSkippedTransmission = true
		}
		if strings.Contains(record.Entry.Message, logMessageForAppliedMoveFileOptimization) {
			logEntryForAppliedMove = true
		}
		suite.NotContains(record.Entry.Message, logMessageForFailedAppliedMoveFileOptimization)
	}
	suite.True(logEntryForSkippedTransmission, "should have found log entry for skipped transmission")
	suite.True(logEntryForAppliedMove, "should have found log entry for applied optimization")
}

func (suite *featuresSuite) TestFileCopyOptimizationImageWorkspaceFile() {
	const expectedLines = 32

	logger, logRecords := zaprec.NewRecorder(zap.DebugLevel)
	suite.e2eConfig.logger = logger

	// Run.
	suite.e2eConfig.flowFilename = path.Join(suite.e2eConfig.contextDir, "flow-file-copy-optimization-image-workspace-file.yaml")
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "run should not fail")

	// Assure the output file is not empty.
	resultRaw, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, "out", "result.txt"))
	suite.Require().NoError(err, "read output file should not fail")
	suite.Len(strings.Split(string(resultRaw), "\n"), expectedLines+1, "should have written correct lines")

	// Assure that the optimization has been applied.
	logEntryForSkippedTransmission := false
	logEntryForAppliedMove := false
	for _, record := range logRecords.Records() {
		if strings.Contains(record.Entry.Message, logMessageForSkippedTransmission) {
			logEntryForSkippedTransmission = true
		}
		if strings.Contains(record.Entry.Message, logMessageForAppliedMoveFileOptimization) {
			logEntryForAppliedMove = true
		}
		suite.NotContains(record.Entry.Message, logMessageForFailedAppliedMoveFileOptimization)
	}
	suite.True(logEntryForSkippedTransmission, "should have found log entry for skipped transmission")
	suite.True(logEntryForAppliedMove, "should have found log entry for applied optimization")
}

func (suite *featuresSuite) TestFileCopyOptimizationProjectActionWorkspaceFile() {
	const expectedLines = 32

	logger, logRecords := zaprec.NewRecorder(zap.DebugLevel)
	suite.e2eConfig.logger = logger

	// Run.
	suite.e2eConfig.flowFilename = path.Join(suite.e2eConfig.contextDir, "flow-file-copy-optimization-proj-action-workspace-file.yaml")
	err := run(suite.T(), suite.e2eConfig)
	suite.Require().NoError(err, "run should not fail")

	// Assure the output file is not empty.
	resultRaw, err := os.ReadFile(path.Join(suite.e2eConfig.contextDir, "out", "result.txt"))
	suite.Require().NoError(err, "read output file should not fail")
	suite.Len(strings.Split(string(resultRaw), "\n"), expectedLines+1, "should have written correct lines")

	// Assure that the optimization has been applied.
	logEntryForSkippedTransmission := false
	logEntryForAppliedMove := false
	for _, record := range logRecords.Records() {
		if strings.Contains(record.Entry.Message, logMessageForSkippedTransmission) {
			logEntryForSkippedTransmission = true
		}
		if strings.Contains(record.Entry.Message, logMessageForAppliedMoveFileOptimization) {
			logEntryForAppliedMove = true
		}
		suite.NotContains(record.Entry.Message, logMessageForFailedAppliedMoveFileOptimization)
	}
	suite.True(logEntryForSkippedTransmission, "should have found log entry for skipped transmission")
	suite.True(logEntryForAppliedMove, "should have found log entry for applied optimization")
}

func TestFeatures(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(featuresSuite))
}

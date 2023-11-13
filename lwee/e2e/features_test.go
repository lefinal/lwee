package e2e

import (
	"context"
	"github.com/docker/docker/api/types"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/stretchr/testify/suite"
	"io"
	"os"
	"path"
	"testing"
)

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

func TestFeatures(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(featuresSuite))
}

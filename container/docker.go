package container

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"github.com/lefinal/nulls"
	"go.uber.org/zap"
	"regexp"
	"strings"
)

var ansiCodesRegexp = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\a|(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~])")

type dockerEngine struct {
	client *client.Client
}

// NewDockerEngine creates a new docker Engine.
func NewDockerEngine() (Engine, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "new docker client", nil)
	}
	return &dockerEngine{client: dockerClient}, nil
}

func (engine *dockerEngine) ImageBuild(ctx context.Context, buildOptions ImageBuildOptions) error {
	type dockerBuildLogEntry struct {
		Stream       nulls.String `json:"stream"`
		Error        nulls.String `json:"error"`
		ErrorDetails nulls.String `json:"errorDetails"`
	}

	logger := buildOptions.BuildLogger
	if logger == nil {
		logger = zap.NewNop()
	}
	imageBuildOptions := types.ImageBuildOptions{
		Dockerfile: buildOptions.File,
		Tags:       []string{buildOptions.Tag},
		Remove:     true,
	}
	tar, err := archive.TarWithOptions(buildOptions.ContextDir, &archive.TarOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "archive build context", meh.Details{"context_dir": buildOptions.ContextDir})
	}
	result, err := engine.client.ImageBuild(ctx, tar, imageBuildOptions)
	if err != nil {
		return meh.Wrap(err, "image build with docker", nil)
	}
	defer func() { _ = result.Body.Close() }()
	// Read logs.
	buildErrMessage := ""
	lineScanner := bufio.NewScanner(result.Body)
	for lineScanner.Scan() {
		// Parse entry.
		var logEntry dockerBuildLogEntry
		err = json.Unmarshal(lineScanner.Bytes(), &logEntry)
		if err != nil {
			err = meh.NewInternalErrFromErr(err, "parse docker log entry", meh.Details{"log_entry": lineScanner.Text()})
			mehlog.Log(logger, err)
			continue
		}
		// Remove ANSI codes.
		logEntry.Stream.String = ansiCodesRegexp.ReplaceAllString(logEntry.Stream.String, "")
		logEntry.Error.String = ansiCodesRegexp.ReplaceAllString(logEntry.Error.String, "")
		// Trim unwanted new lines at the end because the logger always adds one as well.
		logEntry.Stream.String = strings.Trim(logEntry.Stream.String, "\n")
		// Log to respective level.
		if logEntry.Error.Valid {
			logger.Debug(logEntry.Error.String, zap.String("error_details", logEntry.ErrorDetails.String))
			buildErrMessage = logEntry.Error.String
		} else if logEntry.Stream.Valid {
			logger.Debug(logEntry.Stream.String)
		}
	}
	err = lineScanner.Err()
	if err != nil {
		return meh.Wrap(err, "read response", nil)
	}
	if buildErrMessage != "" {
		return meh.NewBadInputErr(buildErrMessage, nil)
	}
	return nil
}

package container

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/go-connections/nat"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"github.com/lefinal/nulls"
	"go.uber.org/zap"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ansiCodesRegexp = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\a|(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~])")

type dockerEngine struct {
	logger       *zap.Logger
	dockerClient *client.Client
	// buildsInProgressByTag holds a map of tags that have ongoing builds. If another
	// build is triggered for the same tag, it will be delayed until the first one is
	// done and then start building. The reason for this is that duplicate builds for
	// the same image can be avoided.
	buildsInProgressByTag     map[string]struct{}
	buildsInProgressByTagCond *sync.Cond
}

// NewDockerEngine creates a new docker Engine.
func NewDockerEngine(logger *zap.Logger) (Engine, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "new docker client", nil)
	}
	return &dockerEngine{
		logger:                    logger,
		dockerClient:              dockerClient,
		buildsInProgressByTag:     make(map[string]struct{}),
		buildsInProgressByTagCond: sync.NewCond(&sync.Mutex{}),
	}, nil
}

func (engine *dockerEngine) ImageBuild(ctx context.Context, buildOptions ImageBuildOptions) error {
	// Wait until no more builds for this tag are ongoing in order to avoid duplicate
	// builds and reuse cached results.
	engine.buildsInProgressByTagCond.L.Lock()
	if _, ok := engine.buildsInProgressByTag[buildOptions.Tag]; ok {
		engine.logger.Debug("delay image build due to ongoing build", zap.String("image_tag", buildOptions.Tag))
		// Wait until build done.
		for {
			if _, ok := engine.buildsInProgressByTag[buildOptions.Tag]; !ok {
				break
			}
			engine.buildsInProgressByTagCond.Wait()
		}
	}
	engine.buildsInProgressByTag[buildOptions.Tag] = struct{}{}
	engine.buildsInProgressByTagCond.L.Unlock()
	defer func() {
		engine.buildsInProgressByTagCond.L.Lock()
		delete(engine.buildsInProgressByTag, buildOptions.Tag)
		engine.buildsInProgressByTagCond.L.Unlock()
		engine.buildsInProgressByTagCond.Broadcast()
	}()
	// No ongoing builds for this tag.
	type dockerBuildLogEntry struct {
		Stream       nulls.String `json:"stream"`
		Error        nulls.String `json:"error"`
		ErrorDetails nulls.String `json:"errorDetails"`
	}

	logger := buildOptions.BuildLogger
	if logger == nil {
		logger = zap.NewNop()
	}
	imageBuildOptions := dockertypes.ImageBuildOptions{
		Dockerfile: buildOptions.File,
		Tags:       []string{buildOptions.Tag},
		Remove:     true,
	}
	engine.logger.Debug("create build archive",
		zap.String("image_tag", buildOptions.Tag),
		zap.String("context_dir", buildOptions.ContextDir))
	tar, err := archive.TarWithOptions(buildOptions.ContextDir, &archive.TarOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "archive build context", meh.Details{"context_dir": buildOptions.ContextDir})
	}
	engine.logger.Debug("build image",
		zap.Strings("image_tags", imageBuildOptions.Tags),
		zap.String("dockerfile", imageBuildOptions.Dockerfile))
	result, err := engine.dockerClient.ImageBuild(ctx, tar, imageBuildOptions)
	if err != nil {
		return meh.Wrap(err, "image build with docker", nil)
	}
	defer func() { _ = result.Body.Close() }()
	// Read logs.
	builtErrorMessage := ""
	builtErrorDetails := ""
	lineScanner := bufio.NewScanner(result.Body)
	var buildLog bytes.Buffer
	buildLog.WriteString("\n")
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
		// Log to respective level.
		if logEntry.Error.Valid {
			buildLog.WriteString(logEntry.Error.String)
			builtErrorMessage = logEntry.Error.String
			builtErrorDetails = logEntry.ErrorDetails.String
		} else if logEntry.Stream.Valid {
			buildLog.WriteString(logEntry.Stream.String)
		}
	}
	err = lineScanner.Err()
	if err != nil {
		return meh.Wrap(err, "read response", nil)
	}
	if builtErrorMessage != "" {
		logger.Error(buildLog.String(),
			zap.String("error_message", builtErrorMessage),
			zap.String("error_details", builtErrorDetails))
		return meh.NewBadInputErr(builtErrorMessage, nil)
	}
	logger.Debug(buildLog.String())
	return nil
}

type ContainerConfig struct {
	ExposedPorts nat.PortSet
	VolumeMounts []VolumeMount
	Command      []string
	Image        string
}

type VolumeMount struct {
	Source string
	Target string
}

func (engine *dockerEngine) CreateContainer(ctx context.Context, containerConfig ContainerConfig) (string, error) {
	// Build container config.
	dockerContainerConfig := &dockercontainer.Config{
		ExposedPorts: containerConfig.ExposedPorts,
		Cmd:          containerConfig.Command,
		Image:        containerConfig.Image,
	}
	dockerContainerHostConfig := &dockercontainer.HostConfig{
		Mounts: make([]mount.Mount, 0),
	}
	for _, volumeMount := range containerConfig.VolumeMounts {
		dockerContainerHostConfig.Mounts = append(dockerContainerHostConfig.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: volumeMount.Source,
			Target: volumeMount.Target,
		})
	}
	// Create the container.
	engine.logger.Debug("create container",
		zap.String("image", containerConfig.Image),
		zap.Strings("command", containerConfig.Command),
		zap.Any("mounts", containerConfig.VolumeMounts))
	response, err := engine.dockerClient.ContainerCreate(ctx, dockerContainerConfig, dockerContainerHostConfig, nil, nil, "")
	if err != nil {
		return "", meh.NewInternalErrFromErr(err, "create container", meh.Details{
			"container_config": dockerContainerConfig,
			"host_config":      dockerContainerHostConfig,
		})
	}
	engine.logger.Debug("container created", zap.String("container_id", response.ID))
	return response.ID, nil
}

func (engine *dockerEngine) StartContainer(ctx context.Context, containerID string) error {
	engine.logger.Debug("start container", zap.String("container_id", containerID))
	err := engine.dockerClient.ContainerStart(ctx, containerID, dockertypes.ContainerStartOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "start container", nil)
	}
	return nil
}

func (engine *dockerEngine) ContainerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	logs, err := engine.dockerClient.ContainerLogs(ctx, containerID, dockertypes.ContainerLogsOptions{
		Since:      time.Time{}.Format(time.RFC3339),
		ShowStdout: true,
		Follow:     true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "get container stdout logs", nil)
	}
	return logs, nil
}

func (engine *dockerEngine) ContainerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	logs, err := engine.dockerClient.ContainerLogs(ctx, containerID, dockertypes.ContainerLogsOptions{
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "get container stderr logs", nil)
	}
	return logs, nil
}

func (engine *dockerEngine) StopContainer(ctx context.Context, containerID string) error {
	engine.logger.Debug("stop container", zap.String("container_id", containerID))
	err := engine.dockerClient.ContainerStop(ctx, containerID, dockercontainer.StopOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stop container", nil)
	}
	return nil
}

func (engine *dockerEngine) WaitForContainerStopped(ctx context.Context, containerID string) error {
	statusCh, errCh := engine.dockerClient.ContainerWait(ctx, containerID, dockercontainer.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return meh.NewInternalErrFromErr(err, "container wait for not running", nil)
	case response := <-statusCh:
		engine.logger.Debug("container now stopped", zap.String("container_id", containerID))
		if response.StatusCode != 0 {
			var errorReportBuilder strings.Builder
			errorReportBuilder.WriteString(fmt.Sprintf("container %s exited with code %d",
				containerID, response.StatusCode))
			stderrLogs, openStderrErr := engine.ContainerStderrLogs(ctx, containerID)
			if openStderrErr != nil {
				errorReportBuilder.WriteString(". stderr logs cannot be retrieved")
			} else {
				errorReportBuilder.WriteString("\n\n******** begin of stderr logs ********\n")
				_, _ = io.Copy(&errorReportBuilder, stderrLogs)
				errorReportBuilder.WriteString("\n******** end of stderr logs ********\n")
			}
			engine.logger.Error(errorReportBuilder.String(), zap.Error(openStderrErr))
			return meh.NewBadInputErr(fmt.Sprintf("container exited with code %d", response.StatusCode), nil)
		}
	}
	return nil
}

func (engine *dockerEngine) RemoveContainer(ctx context.Context, containerID string) error {
	engine.logger.Debug("remove container", zap.String("container_id", containerID))
	err := engine.dockerClient.ContainerRemove(ctx, containerID, dockertypes.ContainerRemoveOptions{Force: true})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "docker container remove", nil)
	}
	return nil
}

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
	// createdContainersByID holds container configurations by their assigned
	// container id. This is useful for more verbose log output like including image
	// names.
	createdContainersByID map[string]Config
	// createdContainersByIDMutex locks createdContainersByID.
	createdContainersByIDMutex sync.RWMutex
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
		createdContainersByID:     map[string]Config{},
	}, nil
}

func (engine *dockerEngine) containerConfigByID(containerID string) Config {
	engine.createdContainersByIDMutex.RLock()
	defer engine.createdContainersByIDMutex.RUnlock()
	config, ok := engine.createdContainersByID[containerID]
	if ok {
		return config
	}
	return Config{
		Image: "<unknown>",
	}
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
	buildLog.WriteString("\n\n")
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

type Config struct {
	ExposedPorts nat.PortSet
	VolumeMounts []VolumeMount
	Command      []string
	Image        string
}

type VolumeMount struct {
	Source string
	Target string
}

func (engine *dockerEngine) ImagePull(ctx context.Context, imageTag string) error {
	logger := engine.logger.Named("image-pull").Named("log").With(zap.String("image_tag", imageTag))
	logger.Debug("pull image")
	r, err := engine.dockerClient.ImagePull(ctx, imageTag, dockertypes.ImagePullOptions{})
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "pull image", meh.Details{"image_tag": imageTag})
	}
	defer func() { _ = r.Close() }()
	// Read log entries and forward to engine logger.
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		m := make(map[string]any)
		err = json.Unmarshal(scanner.Bytes(), &m)
		if err != nil {
			logger.Debug("cannot parse log entry from docker", zap.String("was", scanner.Text()))
			continue
		}
		status := fmt.Sprint(m["status"])
		delete(m, "status")
		logger.Debug(status, zap.Any("log_details", m))
	}
	return nil
}

func (engine *dockerEngine) CreateContainer(ctx context.Context, containerConfig Config) (string, error) {
	// Build container config.
	dockerContainerConfig := &dockercontainer.Config{
		ExposedPorts: containerConfig.ExposedPorts,
		Cmd:          containerConfig.Command,
		Image:        containerConfig.Image,
		StdinOnce:    true,
		OpenStdin:    true,
	}
	dockerContainerHostConfig := &dockercontainer.HostConfig{
		// We need to set auto-remove to false in order to read stdout logs even if it
		// finishes too fast. Otherwise, the logs would not be available.
		AutoRemove: false,
		Mounts:     make([]mount.Mount, 0),
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
	engine.createdContainersByIDMutex.Lock()
	engine.createdContainersByID[response.ID] = containerConfig
	engine.createdContainersByIDMutex.Unlock()
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

func (engine *dockerEngine) ContainerIP(ctx context.Context, containerID string) (string, error) {
	containerDetails, err := engine.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", meh.NewInternalErrFromErr(err, "docker container inspect", nil)
	}
	return containerDetails.NetworkSettings.IPAddress, nil
}

type containerStdinWriteCloser struct {
	hijackedResponse dockertypes.HijackedResponse
}

func (writeCloser *containerStdinWriteCloser) Write(p []byte) (int, error) {
	return writeCloser.hijackedResponse.Conn.Write(p)
}

func (writeCloser *containerStdinWriteCloser) Close() error {
	if err := writeCloser.hijackedResponse.CloseWrite(); err != nil {
		return err
	}
	return writeCloser.hijackedResponse.Conn.Close()
}

func (engine *dockerEngine) ContainerStdin(ctx context.Context, containerID string) (io.WriteCloser, error) {
	response, err := engine.dockerClient.ContainerAttach(ctx, containerID, dockertypes.ContainerAttachOptions{
		Stdin:  true,
		Stream: true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "container attach", nil)
	}
	return &containerStdinWriteCloser{hijackedResponse: response}, nil
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
	// If the container exited due to an error, we might want to read error logs from
	// it. However, if, for example, streams are used, action IO will lead to earlier
	// errors and therefore the passed context is done. Therefore, we start a
	// goroutine that waits with a timeout for the container to stop if the given
	// context is canceled in order to log error results.
	gotResultWithContextErr := make(chan error)
	defer func() { gotResultWithContextErr <- ctx.Err() }()
	go func() {
		contextErr := <-gotResultWithContextErr
		if contextErr == nil {
			// Context not done. Nothing to do.
			return
		}
		// Context was canceled.
		const waitTimeoutDur = 3 * time.Second
		timeout, cancel := context.WithTimeout(context.Background(), waitTimeoutDur)
		defer cancel()
		err := engine.waitForContainerStopped(timeout, containerID)
		if err != nil {
			mehlog.Log(engine.logger, meh.Wrap(err, "wait for container stopped", meh.Details{"container_id": containerID}))
			return
		}
	}()

	return engine.waitForContainerStopped(ctx, containerID)
}

func (engine *dockerEngine) waitForContainerStopped(ctx context.Context, containerID string) error {
	statusCh, errCh := engine.dockerClient.ContainerWait(ctx, containerID, dockercontainer.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return meh.NewInternalErrFromErr(err, "container wait for not running", nil)
	case response := <-statusCh:
		engine.logger.Debug("container now stopped",
			zap.String("container_id", containerID),
			zap.String("image_name", engine.containerConfigByID(containerID).Image))
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
			engine.logger.Error(errorReportBuilder.String(),
				zap.String("image_name", engine.containerConfigByID(containerID).Image),
				zap.Error(openStderrErr))
			return meh.NewBadInputErr(fmt.Sprintf("container exited with code %d", response.StatusCode),
				meh.Details{"image_name": engine.containerConfigByID(containerID).Image})
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
	engine.logger.Debug("container removed", zap.String("container_id", containerID))
	return nil
}

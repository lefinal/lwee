package container

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/stdcopy"
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

type dockerEngineClient struct {
	logger       *zap.Logger
	dockerClient *dockerclient.Client
}

// NewDockerEngine creates a new docker Engine.
func NewDockerEngine(logger *zap.Logger) (Engine, error) {
	dockerClient, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "new docker client", nil)
	}
	return newEngine(logger, &dockerEngineClient{
		logger:       logger,
		dockerClient: dockerClient,
	}), nil
}

func (client *dockerEngineClient) imageExists(ctx context.Context, imageTag string) (bool, error) {
	if strings.HasSuffix(":latest", imageTag) {
		return true, nil
	}

	filters := filters.NewArgs()
	filters.Add("reference", imageTag)
	foundImages, err := client.dockerClient.ImageList(ctx, dockertypes.ImageListOptions{Filters: filters})
	if err != nil {
		return false, meh.NewInternalErrFromErr(err, "image list", meh.Details{"filters": filters})
	}
	return len(foundImages) > 0, nil
}

func (client *dockerEngineClient) imagePull(ctx context.Context, imageTag string) error {
	logger := client.logger.Named("image-pull").Named("log").With(zap.String("image_tag", imageTag))
	r, err := client.dockerClient.ImagePull(ctx, imageTag, dockertypes.ImagePullOptions{})
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "pull image", meh.Details{"image_tag": imageTag})
	}
	defer func() { _ = r.Close() }()
	// Read log entries and forward to client logger.
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

func (client *dockerEngineClient) imageBuild(ctx context.Context, buildOptions ImageBuildOptions) error {
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
	client.logger.Debug("create build archive",
		zap.String("image_tag", buildOptions.Tag),
		zap.String("context_dir", buildOptions.ContextDir))
	tar, err := archive.TarWithOptions(buildOptions.ContextDir, &archive.TarOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "archive build context", meh.Details{"context_dir": buildOptions.ContextDir})
	}
	client.logger.Debug("build image",
		zap.Strings("image_tags", imageBuildOptions.Tags),
		zap.String("dockerfile", imageBuildOptions.Dockerfile))
	result, err := client.dockerClient.ImageBuild(ctx, tar, imageBuildOptions)
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

func (client *dockerEngineClient) createContainer(ctx context.Context, containerConfig Config) (createdContainer, error) {
	// Build container config.
	envVars := make([]string, 0)
	for k, v := range containerConfig.Env {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}
	dockerContainerConfig := &dockercontainer.Config{
		ExposedPorts: containerConfig.ExposedPorts,
		WorkingDir:   containerConfig.WorkingDir,
		Cmd:          containerConfig.Command,
		Image:        containerConfig.Image,
		User:         containerConfig.User,
		StdinOnce:    true,
		OpenStdin:    true,
		Env:          envVars,
	}
	dockerContainerHostConfig := &dockercontainer.HostConfig{
		AutoRemove: containerConfig.AutoRemove,
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
	client.logger.Debug("create container",
		zap.String("image", containerConfig.Image),
		zap.Strings("command", containerConfig.Command),
		zap.Any("mounts", containerConfig.VolumeMounts))
	response, err := client.dockerClient.ContainerCreate(ctx, dockerContainerConfig, dockerContainerHostConfig, nil, nil, "")
	if err != nil {
		return createdContainer{}, meh.NewInternalErrFromErr(err, "create container", meh.Details{
			"container_config": dockerContainerConfig,
			"host_config":      dockerContainerHostConfig,
		})
	}
	containerID := response.ID
	containerDetails, err := client.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return createdContainer{}, meh.NewInternalErrFromErr(err, "inspect container", meh.Details{"container_id": containerID})
	}
	createdContainer := createdContainer{
		name:   containerDetails.Name,
		id:     containerID,
		config: containerConfig,
	}
	createdContainer.logger = client.logger.Named("container").
		With(zap.String("container_name", createdContainer.name),
			zap.String("image_name", createdContainer.config.Image),
			zap.String("container_id", containerID))
	return createdContainer, nil
}

func (client *dockerEngineClient) startContainer(ctx context.Context, containerID string) error {
	err := client.dockerClient.ContainerStart(ctx, containerID, dockertypes.ContainerStartOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "start container", nil)
	}
	return nil
}

func (client *dockerEngineClient) waitForContainerStopped(ctx context.Context, containerID string) containerStopResult {
	stopResult := containerStopResult{}
	statusCh, errCh := client.dockerClient.ContainerWait(ctx, containerID, dockercontainer.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return containerStopResult{error: meh.NewInternalErrFromErr(err, "container wait for not running", nil)}
	case response := <-statusCh:
		stopResult.exitCode = int(response.StatusCode)
		// Handle non-zero exit code.
		if response.StatusCode != 0 {
			stopResult.error = meh.NewBadInputErr(fmt.Sprintf("container exited with code %d", response.StatusCode), nil)
		}
	}
	return stopResult
}

func (client *dockerEngineClient) containerIP(ctx context.Context, containerID string) (string, error) {
	containerDetails, err := client.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", meh.NewInternalErrFromErr(err, "docker container inspect", nil)
	}
	if containerDetails.NetworkSettings.IPAddress == "" {
		return "", meh.NewNotFoundErr("container has no ip", nil)
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

func (client *dockerEngineClient) containerStdin(ctx context.Context, containerID string) (io.WriteCloser, error) {
	response, err := client.dockerClient.ContainerAttach(ctx, containerID, dockertypes.ContainerAttachOptions{
		Stdin:  true,
		Stream: true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "container attach", nil)
	}
	return &containerStdinWriteCloser{hijackedResponse: response}, nil
}

// stdDecoder takes an io.ReadCloser that it reads std-encoded data from and
// provides the encoded data via reader.
type stdDecoder struct {
	stdout bool
	stderr bool
	src    io.ReadCloser
	writer io.WriteCloser
	reader io.ReadCloser

	copyErr      error
	copyErrMutex sync.Mutex
}

func newStdDecoder(src io.ReadCloser, stdout bool, stderr bool) *stdDecoder {
	decoder := &stdDecoder{
		stdout: stdout,
		stderr: stderr,
		src:    src,
	}
	decoder.reader, decoder.writer = io.Pipe()
	return decoder
}

func (decoder *stdDecoder) Read(p []byte) (int, error) {
	decoder.copyErrMutex.Lock()
	if decoder.copyErr != nil {
		decoder.copyErrMutex.Unlock()
		return 0, decoder.copyErr
	}
	decoder.copyErrMutex.Unlock()
	return decoder.reader.Read(p)
}

func (decoder *stdDecoder) Close() error {
	_ = decoder.src.Close()
	_, _ = io.Copy(io.Discard, decoder.reader)
	return nil
}

func (decoder *stdDecoder) copy() {
	destOut := io.Discard
	destErr := io.Discard
	if decoder.stdout {
		destOut = decoder.writer
	}
	if decoder.stderr {
		destErr = decoder.writer
	}
	_, err := stdcopy.StdCopy(destOut, destErr, decoder.src)
	decoder.copyErrMutex.Lock()
	decoder.copyErr = err
	decoder.copyErrMutex.Unlock()
	_ = decoder.writer.Close()
}

func (client *dockerEngineClient) containerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	logs, err := client.dockerClient.ContainerLogs(ctx, containerID, dockertypes.ContainerLogsOptions{
		Since:      time.Time{}.Format(time.RFC3339),
		ShowStdout: true,
		Follow:     true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "get container stdout logs", nil)
	}
	stdDecoder := newStdDecoder(logs, true, false)
	go stdDecoder.copy()
	return stdDecoder, nil
}

func (client *dockerEngineClient) containerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	logs, err := client.dockerClient.ContainerLogs(ctx, containerID, dockertypes.ContainerLogsOptions{
		Since:      time.Time{}.Format(time.RFC3339),
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "get container stderr logs", nil)
	}
	stdDecoder := newStdDecoder(logs, false, true)
	go stdDecoder.copy()
	return stdDecoder, nil
}

func (client *dockerEngineClient) stopContainer(ctx context.Context, containerID string) error {
	err := client.dockerClient.ContainerStop(ctx, containerID, dockercontainer.StopOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stop container", nil)
	}
	return nil
}

func (client *dockerEngineClient) removeContainer(ctx context.Context, containerID string) error {
	err := client.dockerClient.ContainerRemove(ctx, containerID, dockertypes.ContainerRemoveOptions{Force: true})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "docker container remove", nil)
	}
	return nil
}

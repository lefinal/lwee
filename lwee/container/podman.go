package container

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/containers/buildah/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/lefinal/meh"
	"github.com/opencontainers/runtime-spec/specs-go"
	"go.uber.org/zap"
	"io"
	"sync"
	"time"
)

type podmanEngineClient struct {
	logger           *zap.Logger
	podmanConn       context.Context
	stdinOnceWarning sync.Once
}

func NewPodmanEngine(connLifetime context.Context, logger *zap.Logger) (Engine, error) {
	const podmanSocket = "unix:///run/podman/podman.sock"
	podmanConn, err := bindings.NewConnection(connLifetime, podmanSocket)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "connect to podman", meh.Details{"socket": podmanSocket})
	}
	return newEngine(logger, &podmanEngineClient{
		logger:     logger,
		podmanConn: podmanConn,
	}), nil
}

func (client *podmanEngineClient) imagePull(_ context.Context, imageTag string) error {
	pullLogger := client.logger.Named("image-pull").Named("log").With(zap.String("image_tag", imageTag))
	var wg sync.WaitGroup
	pullLogReader, pullLogWriter := io.Pipe()
	// Start log writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(pullLogReader)
		for scanner.Scan() {
			pullLogger.Debug(scanner.Text())
		}
	}()
	// Pull the image.
	pullLogWriterAsIOWriter := io.Writer(pullLogWriter)
	_, err := images.Pull(client.podmanConn, imageTag, &images.PullOptions{
		ProgressWriter: &pullLogWriterAsIOWriter,
	})
	_ = pullLogWriter.Close()
	wg.Wait()
	if err != nil {
		return meh.NewInternalErrFromErr(err, "pull image", meh.Details{"image_tag": imageTag})
	}
	return nil
}

func (client *podmanEngineClient) imageBuild(_ context.Context, buildOptions ImageBuildOptions) error {
	buildLogger := buildOptions.BuildLogger
	if buildLogger == nil {
		buildLogger = zap.NewNop()
	}
	containerFiles := []string{buildOptions.File}
	podmanBuildOptions := entities.BuildOptions{
		BuildOptions: define.BuildOptions{
			ContextDirectory:       buildOptions.ContextDir,
			AdditionalTags:         []string{buildOptions.Tag},
			RemoveIntermediateCtrs: false,
		},
		ContainerFiles: containerFiles,
	}
	client.logger.Debug("build image",
		zap.Strings("image_tags", podmanBuildOptions.BuildOptions.AdditionalTags),
		zap.Strings("container_files", podmanBuildOptions.ContainerFiles))
	// Start build logger.
	var wg sync.WaitGroup
	outLogReader, outLogWriter := io.Pipe()
	errLogReader, errLogWriter := io.Pipe()
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(outLogReader)
		for scanner.Scan() {
			buildLogger.Debug(scanner.Text())
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(errLogReader)
		for scanner.Scan() {
			buildLogger.Error(scanner.Text())
		}
	}()
	// Build image.
	_, err := images.Build(client.podmanConn, containerFiles, podmanBuildOptions)
	_ = outLogWriter.Close()
	_ = errLogWriter.Close()
	wg.Wait()
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "build image", nil)
	}
	return nil
}

func (client *podmanEngineClient) createContainer(_ context.Context, containerConfig Config) (createdContainer, error) {
	// Build container specification.
	containerSpec := specgen.NewSpecGenerator(containerConfig.Image, false)
	containerSpec.Expose = make(map[uint16]string)
	for exposedPort := range containerConfig.ExposedPorts {
		containerSpec.Expose[uint16(exposedPort.Int())] = exposedPort.Proto()
	}
	containerSpec.Command = containerConfig.Command
	containerSpec.Stdin = true
	// We need to set auto-remove to false in order to read stdout logs even if it
	// finishes too fast. Otherwise, the logs would not be available.
	containerSpec.Remove = false
	for _, volumeMount := range containerConfig.VolumeMounts {
		containerSpec.Mounts = append(containerSpec.Mounts, specs.Mount{
			Destination: volumeMount.Target,
			Type:        "bind",
			Source:      volumeMount.Source,
		})
	}
	// Create the container.
	client.logger.Debug("create container",
		zap.String("image", containerConfig.Image),
		zap.Strings("command", containerConfig.Command),
		zap.Any("mounts", containerConfig.VolumeMounts))
	response, err := containers.CreateWithSpec(client.podmanConn, containerSpec, &containers.CreateOptions{})
	if err != nil {
		return createdContainer{}, meh.NewInternalErrFromErr(err, "create container", meh.Details{
			"container_spec": containerSpec,
		})
	}
	containerID := response.ID
	containerDetails, err := containers.Inspect(client.podmanConn, containerID, &containers.InspectOptions{})
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

func (client *podmanEngineClient) startContainer(_ context.Context, containerID string) error {
	err := containers.Start(client.podmanConn, containerID, &containers.StartOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "start container", nil)
	}
	return nil
}

func (client *podmanEngineClient) waitForContainerStopped(ctx context.Context, containerID string) containerStopResult {
	exitCode, err := containers.Wait(client.podmanConn, containerID, &containers.WaitOptions{})
	if err != nil {
		return containerStopResult{error: meh.NewInternalErrFromErr(err, "wait for container", nil)}
	}
	stopResult := containerStopResult{}
	// Handle non-zero exit code.
	if exitCode != 0 {
		stopResult.error = meh.NewBadInputErr(fmt.Sprintf("container exited with code %d", exitCode), nil)
		stderrLogsReader, openStderrErr := client.containerStderrLogs(ctx, containerID)
		if openStderrErr != nil {
			stopResult.error = meh.ApplyDetails(stopResult.error, meh.Details{"retrieve_stderr_logs_err": openStderrErr})
			stopResult.stderrLogs = "<stderr logs cannot be retrieved>"
		} else {
			var stderrLogs bytes.Buffer
			_, _ = stdcopy.StdCopy(&stderrLogs, &stderrLogs, stderrLogsReader)
			stopResult.stderrLogs = stderrLogs.String()
		}
	}
	return stopResult
}

func (client *podmanEngineClient) containerIP(_ context.Context, containerID string) (string, error) {
	containerDetails, err := containers.Inspect(client.podmanConn, containerID, &containers.InspectOptions{})
	if err != nil {
		return "", meh.NewInternalErrFromErr(err, "docker container inspect", nil)
	}
	if containerDetails.NetworkSettings.IPAddress == "" {
		return "", meh.NewNotFoundErr("container has no ip", nil)
	}
	return containerDetails.NetworkSettings.IPAddress, nil
}

func (client *podmanEngineClient) containerStdin(ctx context.Context, containerID string) (io.WriteCloser, error) {
	// Currently, stdin-once is not supported by Podman. This means that if an
	// application expects stdin to be closed, it will hang forever.
	client.stdinOnceWarning.Do(func() {
		client.logger.Warn("due to limitations in podman, stdin is never closed. if an application relies on stdin being closed, it will hang forever.")
	})
	// Attach.
	stdinReader, stdinWriter := io.Pipe()
	attachReady := make(chan bool)
	err := containers.Attach(client.podmanConn, containerID, stdinReader, nil, nil, attachReady, &containers.AttachOptions{
		Stream: ref(true),
	})
	if err != nil {
		return nil, meh.Wrap(err, "attach to container", nil)
	}
	// Wait for ready.
	select {
	case <-ctx.Done():
		return nil, meh.NewInternalErrFromErr(ctx.Err(), "wait for container attach ready", nil)
	case <-attachReady:
	}
	return stdinWriter, nil
}

// podmanLogReader takes a channel with log entries and implements io.ReadCloser
// that provides the received log entries.
type podmanLogReader struct {
	lifetime      context.Context
	cancel        context.CancelCauseFunc
	logEntries    chan string
	closeCallback func()
	buf           bytes.Buffer
}

func newPodmanLogReader(lifetime context.Context, logEntries chan string, closeCallback func()) *podmanLogReader {
	lifetime, cancel := context.WithCancelCause(lifetime)
	return &podmanLogReader{
		lifetime:      lifetime,
		cancel:        cancel,
		logEntries:    logEntries,
		closeCallback: closeCallback,
	}
}

func (reader *podmanLogReader) Read(p []byte) (int, error) {
	if reader.buf.Len() == 0 {
		// Read next log entry.
		select {
		case <-reader.lifetime.Done():
			err := context.Cause(reader.lifetime)
			if !errors.Is(err, context.Canceled) {
				return 0, err
			}
			return 0, io.EOF
		case logEntry := <-reader.logEntries:
			reader.buf.WriteString(logEntry)
		}
	}
	return reader.buf.Read(p)
}

func (reader *podmanLogReader) stopWithError(err error) {
	reader.cancel(err)
}

func (reader *podmanLogReader) Close() error {
	reader.closeCallback()
	return nil
}

func (client *podmanEngineClient) containerStdoutLogs(_ context.Context, containerID string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(client.podmanConn)
	stdoutLogs := make(chan string)
	stdoutLogReader := newPodmanLogReader(ctx, stdoutLogs, cancel)

	go func() {
		err := containers.Logs(ctx, containerID, &containers.LogOptions{
			Follow: ref(true),
			Since:  ref(time.Time{}.Format(time.RFC3339)),
			Stderr: ref(false),
			Stdout: ref(true),
		}, stdoutLogs, nil)
		if err != nil {
			err = meh.NewInternalErrFromErr(err, "get container stdout logs", nil)
			stdoutLogReader.stopWithError(err)
		}
		stdoutLogReader.stopWithError(nil)
	}()

	return stdoutLogReader, nil
}

func (client *podmanEngineClient) containerStderrLogs(_ context.Context, containerID string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(client.podmanConn)
	stderrLogs := make(chan string)
	stderrLogReader := newPodmanLogReader(ctx, stderrLogs, cancel)

	go func() {
		err := containers.Logs(ctx, containerID, &containers.LogOptions{
			Follow: ref(true),
			Since:  ref(time.Time{}.Format(time.RFC3339)),
			Stderr: ref(true),
			Stdout: ref(false),
		}, nil, stderrLogs)
		if err != nil {
			err = meh.NewInternalErrFromErr(err, "get container stderr logs", nil)
			stderrLogReader.stopWithError(err)
		}
		stderrLogReader.stopWithError(nil)
	}()

	return stderrLogReader, nil
}

func (client *podmanEngineClient) stopContainer(_ context.Context, containerID string) error {
	err := containers.Stop(client.podmanConn, containerID, &containers.StopOptions{})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stop container", nil)
	}
	return nil
}

func (client *podmanEngineClient) removeContainer(_ context.Context, containerID string) error {
	_, err := containers.Remove(client.podmanConn, containerID, &containers.RemoveOptions{Force: ref(true)})
	if err != nil {
		return meh.NewInternalErrFromErr(err, "remove container", nil)
	}
	return nil
}

func ref[T any](t T) *T {
	return &t
}

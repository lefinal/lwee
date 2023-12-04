// Package container abstracts usage of container runtimes.
package container

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/docker/go-connections/nat"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"io"
	"strings"
	"sync"
	"time"
)

// EngineType describes the type of container engine to use.
type EngineType string

// Engine types.
const (
	EngineTypeDocker EngineType = "docker"
	EngineTypePodman EngineType = "podman"
)

// ImageBuildOptions are the options for building an image.
type ImageBuildOptions struct {
	// BuildLogger for build log. If nil, build log is not printed.
	BuildLogger *zap.Logger
	Tag         string
	ContextDir  string
	File        string
}

// Config is the configuration for a container.
type Config struct {
	ExposedPorts nat.PortSet
	VolumeMounts []VolumeMount
	WorkingDir   string
	Command      []string
	Image        string
	User         string
	Env          map[string]string
	// Whether to remove the container after it exited. Normally, we need to set this
	// to false, so we can read stdout logs even if it finishes too fast. Otherwise,
	// the logs would not be available or waiting for the container to finish might
	// fail as it is deleted.
	AutoRemove bool
}

// VolumeMount describes a mount point for a volume.
type VolumeMount struct {
	Source string
	Target string
}

// Engine is an interface for managing a container engine.
type Engine interface {
	// Start the engine. This includes starting clean-up procedures.
	Start(ctx context.Context) error
	// Stop the engine and run clean-ups.
	Stop()
	ImagePull(ctx context.Context, imageTag string) error
	ImageBuild(ctx context.Context, buildOptions ImageBuildOptions) error
	CreateContainer(ctx context.Context, containerConfig Config) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	ContainerIP(ctx context.Context, containerID string) (string, error)
	ContainerStdin(ctx context.Context, containerID string) (io.WriteCloser, error)
	ContainerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	ContainerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	StopContainer(ctx context.Context, containerID string) error
	WaitForContainerStopped(ctx context.Context, containerID string) error
	RemoveContainer(ctx context.Context, containerID string) error
	RunContainer(ctx context.Context, containerConfig Config) error
}

// NewEngine creates a new Engine for the given EngineType.
func NewEngine(logger *zap.Logger, engineType EngineType, disableCleanup bool) (Engine, error) {
	var engine Engine
	var err error
	switch engineType {
	case EngineTypeDocker:
		engine, err = NewDockerEngine(logger.Named("docker"), disableCleanup)
		if err != nil {
			return nil, meh.Wrap(err, "new docker engine", nil)
		}
	case EngineTypePodman:
		return nil, meh.NewBadInputErr("podman support was removed due to lacking features in the bindings", nil)
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported engine type: %v", engineType), nil)
	}
	return engine, nil
}

// engineClient abstracts a container engine so that common logic can be placed
// within engine.
type engineClient interface {
	imageExists(ctx context.Context, imageTag string) (bool, error)
	imagePull(ctx context.Context, imageTag string) error
	imageBuild(ctx context.Context, buildOptions ImageBuildOptions) error
	createContainer(ctx context.Context, containerConfig Config) (createdContainer, error)
	startContainer(ctx context.Context, containerID string) error
	waitForContainerStopped(ctx context.Context, containerID string) containerStopResult
	containerIP(ctx context.Context, containerID string) (string, error)
	containerStdin(ctx context.Context, containerID string) (io.WriteCloser, error)
	containerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	containerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	stopContainer(ctx context.Context, containerID string) error
	removeContainer(ctx context.Context, containerID string) error
}

// createdContainer represents a container that has been created by the container
// engine. This is used for centralized generation of error details, logging,
// etc.
type createdContainer struct {
	logger *zap.Logger
	name   string
	id     string
	config Config
}

// containerStopResult is returned by client.waitForContainerStopped and holds
// any occurred error as well as potential container log output.
type containerStopResult struct {
	error    error
	exitCode int
}

// mehDetails returns meh.Details for the createdContainer.
func (container createdContainer) mehDetails() meh.Details {
	return meh.Details{
		"container_name":  container.name,
		"container_image": container.config.Image,
		"container_id":    container.id,
	}
}

// engine wraps a client for avoiding duplicate code for common logic. This
// includes extended error details or deduplicated image builds.
type engine struct {
	logger     *zap.Logger
	client     engineClient
	wg         sync.WaitGroup
	cleanUpper cleanUpper
	// buildsInProgressByTag holds a map of tags that have ongoing builds. If another
	// build is triggered for the same tag, it will be delayed until the first one is
	// done and then start building. The reason for this is that duplicate builds for
	// the same image can be avoided.
	buildsInProgressByTag     map[string]struct{}
	buildsInProgressByTagCond *sync.Cond
	// createdContainersByID holds container configurations by their assigned
	// container id. This is useful for more verbose log output like including image
	// names.
	createdContainersByID map[string]createdContainer
	// createdContainersByIDMutex locks createdContainersByID.
	createdContainersByIDMutex sync.RWMutex
}

func newEngine(logger *zap.Logger, client engineClient, disableCleanup bool) *engine {
	cleanUpper := newNopCleanUpper()
	if !disableCleanup {
		newCleanUpper(logger.Named("cleanup"), client)
	}
	return &engine{
		logger:                    logger,
		client:                    client,
		cleanUpper:                cleanUpper,
		buildsInProgressByTag:     make(map[string]struct{}),
		buildsInProgressByTagCond: sync.NewCond(&sync.Mutex{}),
		createdContainersByID:     map[string]createdContainer{},
	}
}

// Start starts the engine and any possible clean-upper.
func (engine *engine) Start(ctx context.Context) error {
	err := engine.cleanUpper.start(ctx)
	if err != nil {
		return meh.Wrap(err, "start clean-upper", nil)
	}
	return nil
}

// Stop stops the engine by performing clean-ups and waiting for all goroutines
// to finish.
func (engine *engine) Stop() {
	engine.cleanUpper.stop()
	engine.wg.Wait()
}

// createdContainerByID retrieves a createdContainer configuration by its
// assigned container ID. If the container ID does not exist in the map, an empty
// createdContainer is returned.
func (engine *engine) createdContainerByID(containerID string) createdContainer {
	engine.createdContainersByIDMutex.RLock()
	defer engine.createdContainersByIDMutex.RUnlock()
	config, ok := engine.createdContainersByID[containerID]
	if ok {
		return config
	}
	return createdContainer{
		logger: engine.logger.Named("unknown-container"),
		name:   "<not_created>",
		config: Config{
			Image: "<unknown>",
		},
	}
}

// ImagePull pulls the specified image if it does not already exist in the
// engine. It returns an error if the operation fails.
func (engine *engine) ImagePull(ctx context.Context, imageTag string) error {
	imageExists, err := engine.client.imageExists(ctx, imageTag)
	if err != nil {
		return meh.Wrap(err, "check if image exists", meh.Details{"image_tag": imageTag})
	}
	if imageExists {
		return nil
	}
	engine.logger.Debug("pull image", zap.String("image_tag", imageTag))
	return engine.client.imagePull(ctx, imageTag)
}

// ImageBuild builds a Docker image with the specified build options.
//
// It waits until no more builds for the specified tag are ongoing to avoid
// duplicate builds and reuse cached results. If a build for the same tag is
// ongoing, it waits until the build is done. If there are no ongoing builds for
// the specified tag, it builds the image using the engine.
//
// Once the build is started, the ongoing build is tracked. This ensures that the
// build is not started multiple times for the same tag.
func (engine *engine) ImageBuild(ctx context.Context, buildOptions ImageBuildOptions) error {
	// Wait until no more builds for this tag are ongoing to avoid duplicate builds
	// and reuse cached results.
	engine.buildsInProgressByTagCond.L.Lock()
	if _, ok := engine.buildsInProgressByTag[buildOptions.Tag]; ok {
		engine.logger.Debug("delay image build due to ongoing build", zap.String("image_tag", buildOptions.Tag))
		// Wait until the build is done.
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
	return engine.client.imageBuild(ctx, buildOptions)
}

// CreateContainer creates a container with the given container configuration. It
// returns the ID of the created container and an error, if any.
func (engine *engine) CreateContainer(ctx context.Context, containerConfig Config) (string, error) {
	createdContainer, err := engine.client.createContainer(ctx, containerConfig)
	if err != nil {
		return "", meh.Wrap(err, "create container with client", nil)
	}
	createdContainer.logger.Debug("container created")
	engine.cleanUpper.registerContainer(createdContainer.name)
	engine.createdContainersByIDMutex.Lock()
	engine.createdContainersByID[createdContainer.id] = createdContainer
	engine.createdContainersByIDMutex.Unlock()
	return createdContainer.id, nil
}

// StartContainer starts a container with the specified container ID. It logs
// container start and error details if the container exits with an error.
func (engine *engine) StartContainer(ctx context.Context, containerID string) error {
	container := engine.createdContainerByID(containerID)
	container.logger.Debug("start container")
	err := engine.client.startContainer(ctx, containerID)
	if err != nil {
		return meh.Wrap(err, "start container with client", container.mehDetails())
	}

	var containerStderrLogs bytes.Buffer

	engine.wg.Add(1)
	containerLogsDone := make(chan struct{})
	go func() {
		defer engine.wg.Done()
		defer close(containerLogsDone)
		stderrLogs, err := engine.client.containerStderrLogs(ctx, containerID)
		if err != nil {
			mehlog.Log(engine.logger, meh.Wrap(err, "get container stderr logs", nil))
			return
		}
		defer func() { _ = stderrLogs.Close() }()
		stderrScanner := bufio.NewScanner(stderrLogs)
		stderrLogger := container.logger.Named("stderr")
		for stderrScanner.Scan() {
			stderrLogger.Debug(stderrScanner.Text())
			containerStderrLogs.Write(stderrScanner.Bytes())
		}
		err = stderrScanner.Err()
		if err != nil {
			mehlog.Log(engine.logger, meh.Wrap(err, "read container stderr logs", nil))
			return
		}
	}()
	// If the container exited due to an error, we might want to read error logs from
	// it. However, if, for example, streams are used, action IO will lead to earlier
	// errors and therefore the passed context is done. Therefore, we start a
	// goroutine that waits with a timeout for the container to stop if the given
	// context is canceled to log error results.
	waitForContainerStoppedCtx, cancelWaitForContainerStopped := context.WithCancel(context.Background())
	containerStopped := make(chan struct{})
	engine.wg.Add(1)
	go func() {
		defer engine.wg.Done()
		defer cancelWaitForContainerStopped()
		const waitTimeoutDur = 3 * time.Second
		select {
		case <-ctx.Done():
			<-time.After(waitTimeoutDur)
		case <-containerStopped:
		}
	}()
	engine.wg.Add(1)
	go func() {
		defer engine.wg.Done()
		defer close(containerStopped)
		result := engine.client.waitForContainerStopped(waitForContainerStoppedCtx, containerID)
		container.logger.Debug("container exited", zap.Int("exit_code", result.exitCode))
		if result.error != nil {
			// Provide error details for easier debugging.
			<-containerLogsDone
			var errorReportBuilder strings.Builder
			errorReportBuilder.WriteString(fmt.Sprintf("container exited with code %d", result.exitCode))
			errorReportBuilder.WriteString("\n")
			errorReportBuilder.WriteString("\n******** begin of stderr logs ********\n")
			_, _ = io.Copy(&errorReportBuilder, &containerStderrLogs)
			errorReportBuilder.WriteString("\n******** end of stderr logs ********\n")
			errorReportBuilder.WriteString("\n")
			container.logger.Error(errorReportBuilder.String())
			mehlog.Log(container.logger, meh.Wrap(err, "wait for container stopped", container.mehDetails()))
			return
		}
	}()

	return nil
}

// ContainerIP returns the IP address of the container identified by given ID.
func (engine *engine) ContainerIP(ctx context.Context, containerID string) (string, error) {
	container := engine.createdContainerByID(containerID)
	ip, err := engine.client.containerIP(ctx, containerID)
	if err != nil {
		return "", meh.Wrap(err, "get container ip with client", container.mehDetails())
	}
	return ip, nil
}

// ContainerStdin returns an io.WriteCloser that can be used to attach to a
// container's stdin.
func (engine *engine) ContainerStdin(ctx context.Context, containerID string) (io.WriteCloser, error) {
	container := engine.createdContainerByID(containerID)
	stdin, err := engine.client.containerStdin(ctx, containerID)
	if err != nil {
		return nil, meh.Wrap(err, "attach container stdin with client", container.mehDetails())
	}
	return stdin, nil
}

// ContainerStdoutLogs returns the stdout logs for the specified container. It
// attaches to the container's stdout logs using the engine client.
func (engine *engine) ContainerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	container := engine.createdContainerByID(containerID)
	stdoutLogs, err := engine.client.containerStdoutLogs(ctx, containerID)
	if err != nil {
		return nil, meh.Wrap(err, "attach container stdout logs with client", container.mehDetails())
	}
	return stdoutLogs, nil
}

// ContainerStderrLogs returns the stderr logs for the specified container.
func (engine *engine) ContainerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	container := engine.createdContainerByID(containerID)
	stderrLogs, err := engine.client.containerStderrLogs(ctx, containerID)
	if err != nil {
		return nil, meh.Wrap(err, "attach container stderr logs with client", container.mehDetails())
	}
	return stderrLogs, nil
}

// StopContainer stops the container with the specified ID.
func (engine *engine) StopContainer(ctx context.Context, containerID string) error {
	container := engine.createdContainerByID(containerID)
	container.logger.Debug("stop container")
	err := engine.client.stopContainer(ctx, containerID)
	if err != nil {
		return meh.Wrap(err, "stop container with client", container.mehDetails())
	}
	container.logger.Debug("container stopped")
	return nil
}

// WaitForContainerStopped waits for the specified container to be stopped.
func (engine *engine) WaitForContainerStopped(ctx context.Context, containerID string) error {
	container := engine.createdContainerByID(containerID)
	result := engine.client.waitForContainerStopped(ctx, containerID)
	if result.error != nil {
		return meh.ApplyDetails(result.error, container.mehDetails())
	}
	return nil
}

// RemoveContainer removes a container with the specified containerID.
func (engine *engine) RemoveContainer(ctx context.Context, containerID string) error {
	container := engine.createdContainerByID(containerID)
	container.logger.Debug("remove container")
	err := engine.client.removeContainer(ctx, containerID)
	if err != nil {
		return meh.Wrap(err, "remove container with client", container.mehDetails())
	}
	container.logger.Debug("container removed")
	return nil
}

// RunContainer runs a container with the given container configuration. It
// blocks until the container has exited.
func (engine *engine) RunContainer(ctx context.Context, containerConfig Config) error {
	containerID, err := engine.CreateContainer(ctx, containerConfig)
	if err != nil {
		return meh.Wrap(err, "create container", nil)
	}
	defer func() { _ = engine.StopContainer(ctx, containerID) }()
	err = engine.StartContainer(ctx, containerID)
	if err != nil {
		return meh.Wrap(err, "start container", nil)
	}
	result := engine.client.waitForContainerStopped(ctx, containerID)
	if result.error != nil {
		return meh.Wrap(result.error, "wait for container stopped", nil)
	}
	return nil
}

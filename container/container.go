package container

import (
	"context"
	"fmt"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
)

type EngineType string

const (
	EngineTypeDocker EngineType = "docker"
	EngineTypePodman EngineType = "podman"
)

type Engine interface {
	ImageBuild(ctx context.Context, properties ImageBuildOptions) error
	CreateContainer(ctx context.Context, containerConfig ContainerConfig) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	ContainerStdoutLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	ContainerStderrLogs(ctx context.Context, containerID string) (io.ReadCloser, error)
	StopContainer(ctx context.Context, containerID string) error
	WaitForContainerStopped(ctx context.Context, containerID string) error
	RemoveContainer(ctx context.Context, containerID string) error
}

func NewEngine(logger *zap.Logger, engineType EngineType) (Engine, error) {
	var engine Engine
	var err error
	switch engineType {
	case EngineTypeDocker:
		engine, err = NewDockerEngine(logger.Named("docker"))
		if err != nil {
			return nil, meh.Wrap(err, "new docker engine", nil)
		}
	case EngineTypePodman:
		panic("implement me") // TODO
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported engine type: %v", engineType), nil)
	}
	return engine, nil
}

type ImageBuildOptions struct {
	// BuildLogger for build log. If nil, build log is not printed.
	BuildLogger *zap.Logger
	Tag         string
	ContextDir  string
	File        string
}

func ProjectActionImageTag(flowName string, actionName string) string {
	return fmt.Sprintf("lwee_%s_%s", flowName, actionName)
}

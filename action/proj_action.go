package action

import (
	"context"
	"fmt"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/lefinal/lwee/container"
	"github.com/lefinal/lwee/locator"
	"github.com/lefinal/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lweeprojactionfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type containerState int

const (
	containerStateReady containerState = iota
	containerStateRunning
	containerStateDone
)

func (factory *Factory) newProjectAction(base *Base, actionFile lweeflowfile.ActionRunnerProjectAction) (action, error) {
	// Assure action exists.
	actionDir := factory.Locator.ProjectActionDirByAction(actionFile.Name)
	_, err := os.Stat(actionDir)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "stat action dir", meh.Details{"action_dir": actionDir})
	}
	actionLocator := factory.Locator.ProjectActionLocatorByAction(actionFile.Name)
	// Read action.
	projectActionFile, err := lweeprojactionfile.FromFile(actionLocator.ActionFilename())
	if err != nil {
		return nil, meh.Wrap(err, "project action file from file",
			meh.Details{"filename": actionLocator.ActionFilename()})
	}
	if actionFile.Config == "" {
		actionFile.Config = "default"
	}
	actionConfig, ok := projectActionFile.Configs[actionFile.Config]
	if !ok {
		knownConfigs := make([]string, 0)
		for configName := range projectActionFile.Configs {
			knownConfigs = append(knownConfigs, configName)
		}
		return nil, meh.NewBadInputErr(fmt.Sprintf("unknown action config: %s", actionFile.Config),
			meh.Details{"known_configs": knownConfigs})
	}
	switch actionConfig := actionConfig.(type) {
	case lweeprojactionfile.ProjActionConfigImage:
		if actionConfig.File == "" {
			actionConfig.File = "Dockerfile"
		}
		return &projectActionImage{
			Base:                  base,
			containerEngine:       factory.ContainerEngine,
			contextDir:            actionDir,
			file:                  actionConfig.File,
			tag:                   projectActionImageTag(factory.FlowName, actionFile.Name),
			args:                  actionFile.Args,
			containerWorkspaceDir: path.Join(factory.Locator.ActionTempDirByAction(actionFile.Name), "container-workspace"),
			containerState:        containerStateReady,
			containerRunningCond:  sync.NewCond(&sync.Mutex{}),
		}, nil
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported action config: %T", actionConfig), nil)
	}
}

// projectActionImage is an Action that runs a project action with type image.
type projectActionImage struct {
	*Base
	containerEngine       container.Engine
	contextDir            string
	file                  string
	tag                   string
	args                  string
	containerWorkspaceDir string
	containerID           string
	// containerState is the state of the container. It is locked using
	// containerRunningCond.
	containerState containerState
	// containerRunningCond locks containerState.
	containerRunningCond *sync.Cond
}

func (action *projectActionImage) Build(ctx context.Context) error {
	imageBuildOptions := container.ImageBuildOptions{
		BuildLogger: action.logger.Named("build"),
		Tag:         action.tag,
		ContextDir:  action.contextDir,
		File:        action.file,
	}
	start := time.Now()
	action.logger.Debug("build project action image",
		zap.String("image_tag", imageBuildOptions.Tag),
		zap.String("context_dir", imageBuildOptions.ContextDir),
		zap.String("file", imageBuildOptions.File))
	err := action.containerEngine.ImageBuild(ctx, imageBuildOptions)
	if err != nil {
		return meh.Wrap(err, "build image", meh.Details{"image_build_options": imageBuildOptions})
	}
	action.logger.Debug("project action image build done", zap.Duration("took", time.Since(start)))
	return nil
}

func (action *projectActionImage) registerInputIngestionRequests() error {
	for inputName, input := range action.fileActionInputs {
		var inputRequest inputIngestionRequestWithIngestor
		switch input := input.(type) {
		case lweeflowfile.ActionInputContainerWorkspaceFile:
			inputRequest = action.newContainerWorkspaceFileInputRequest(input)
			// TODO: Add wait-flag if input/output has SDK
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action input %q has unsupported type: %s", inputName, input.Type()), nil)
		}
		inputRequest.request.InputName = inputName
		action.inputIngestionRequestsByInputName[inputName] = inputRequest
	}
	return nil
}

func (action *projectActionImage) registerOutputProviders() error {
	stdoutOutputRegistered := false
	for outputName, output := range action.fileActionOutputs {
		var outputOffer OutputOfferWithOutputter
		switch output := output.(type) {
		case lweeflowfile.ActionOutputContainerWorkspaceFile:
			outputOffer = action.newContainerWorkspaceFileOutputOffer(output)
		case lweeflowfile.ActionOutputStdout:
			// Assure only one output with stdout-type.
			if stdoutOutputRegistered {
				return meh.NewBadInputErr("duplicate stdout outputs. only one is allowed.", nil)
			}
			stdoutOutputRegistered = true
			outputOffer = action.newStdoutOutputOffer()
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action output %s has unsupported type: %s", outputName, output.Type()), nil)
		}
		outputOffer.offer.OutputName = outputName
		action.outputOffersByOutputName[outputName] = outputOffer

		// TODO: output stuff. and what to do with outputs that are not needed? who notifies? and we need to distinguish between outputs while running and outputs after having stopped.
	}
	return nil
}

func (action *projectActionImage) newContainerWorkspaceFileInputRequest(input lweeflowfile.ActionInputContainerWorkspaceFile) inputIngestionRequestWithIngestor {
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			IngestionPhase: PhasePreStart,
			SourceName:     input.Source,
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			filename := path.Join(action.containerWorkspaceDir, input.Filename)
			err := os.MkdirAll(path.Dir(filename), 0750)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "mkdir all", meh.Details{"dir": path.Dir(filename)})
			}
			f, err := os.Create(filename)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "create container workspace file", meh.Details{"filename": filename})
			}
			defer func() { _ = f.Close() }()
			_, err = io.Copy(f, source)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "write container workspace file", meh.Details{"filename": filename})
			}
			err = f.Close()
			if err != nil {
				return meh.NewInternalErrFromErr(err, "close written container workspace file", meh.Details{"filename": filename})
			}
			return nil
		},
	}
}

func (action *projectActionImage) waitForPhase(ctx context.Context, state containerState) error {
	action.containerRunningCond.L.Lock()
	for {
		if action.containerState >= state {
			break
		}
		select {
		case <-ctx.Done():
			return meh.NewBadInputErrFromErr(ctx.Err(), "context done while waiting for container running", nil)
		default:
		}
		action.containerRunningCond.Wait()
	}
	action.containerRunningCond.L.Unlock()
	return nil
}

func (action *projectActionImage) newContainerWorkspaceFileOutputOffer(output lweeflowfile.ActionOutputContainerWorkspaceFile) OutputOfferWithOutputter {
	return OutputOfferWithOutputter{
		offer: OutputOffer{
			OutputPhase: PhaseStopped,
		},
		output: func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for container stopped.
			err := action.waitForPhase(ctx, containerStateDone)
			if err != nil {
				return meh.Wrap(err, "wait for container done", nil)
			}
			filename := path.Join(action.containerWorkspaceDir, output.Filename)
			action.logger.Debug("container done. now providing container workspace file output.",
				zap.String("filename", filename))
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "notify output ready", nil)
			case ready <- struct{}{}:
			}
			// Copy file.
			f, err := os.Open(filename)
			if err != nil {
				return meh.NewBadInputErrFromErr(err, "open container-workspace output file",
					meh.Details{"filename": filename})
			}
			defer func() { _ = f.Close() }()
			_, err = io.Copy(writer, f)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "copy container-workspace output file",
					meh.Details{"filename": filename})
			}
			return nil
		},
	}
}

func (action *projectActionImage) newStdoutOutputOffer() OutputOfferWithOutputter {
	return OutputOfferWithOutputter{
		offer: OutputOffer{
			OutputPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for container running.
			err := action.waitForPhase(ctx, containerStateRunning)
			if err != nil {
				return meh.Wrap(err, "wait for container running", nil)
			}
			// Open stdout.
			action.logger.Debug("container now running. opening stdout output.")
			stdoutReader, err := action.containerEngine.ContainerStdoutLogs(ctx, action.containerID)
			if err != nil {
				return meh.Wrap(err, "open container stdout logs", meh.Details{"container_id": action.containerID})
			}
			defer func() { _ = stdoutReader.Close() }()
			// Notify output ready.
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "notify output open", nil)
			case ready <- struct{}{}:
			}
			// Forward.
			_, err = stdcopy.StdCopy(writer, io.Discard, stdoutReader)
			if err != nil {
				return meh.Wrap(err, "copy stdout logs", nil)
			}
			action.logger.Debug("read stdout logs done")
			return nil
		},
	}
}

func (action *projectActionImage) setContainerState(newState containerState) {
	action.containerRunningCond.L.Lock()
	action.containerState = newState
	action.containerRunningCond.L.Unlock()
	action.containerRunningCond.Broadcast()
}

func (action *projectActionImage) Start(ctx context.Context) (<-chan error, error) {
	var err error
	containerConfig := container.ContainerConfig{
		ExposedPorts: nil,
		VolumeMounts: []container.VolumeMount{
			{
				Source: action.containerWorkspaceDir,
				Target: "/lwee",
			},
		},
		Command: nil,
		Image:   action.tag,
	}
	action.logger.Debug("create container",
		zap.String("image", action.tag),
		zap.Any("volumes", containerConfig.VolumeMounts))
	action.containerID, err = action.containerEngine.CreateContainer(ctx, containerConfig)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create container", meh.Details{"container_config": containerConfig})
	}
	err = action.containerEngine.StartContainer(ctx, action.containerID)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "start container", meh.Details{"container_id": action.containerID})
	}
	action.setContainerState(containerStateRunning)
	stopped := make(chan error)
	go func() {
		defer func() { _ = action.containerEngine.RemoveContainer(ctx, action.containerID) }()
		err := action.containerEngine.WaitForContainerStopped(ctx, action.containerID)
		action.setContainerState(containerStateDone)
		if err != nil {
			stopped <- meh.NewInternalErrFromErr(err, "wait for container stopped", meh.Details{"container_id": action.containerID})
			return
		}
		stopped <- nil
	}()
	// TODO: wait for sdk if flag is set
	return stopped, nil
}

func (action *projectActionImage) Stop(ctx context.Context) error {
	if action.containerID == "" {
		return nil
	}
	err := action.containerEngine.StopContainer(ctx, action.containerID)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stop container", meh.Details{"container_id": action.containerID})
	}
	return nil
}

func projectActionImageTag(flowName string, actionName string) string {
	const replaceNonAlphanumericWith = '_'
	flowName = locator.ToAlphanumeric(flowName, replaceNonAlphanumericWith)
	flowName = strings.ToLower(flowName)
	actionName = locator.ToAlphanumeric(actionName, replaceNonAlphanumericWith)
	actionName = strings.ToLower(actionName)
	return fmt.Sprintf("lwee__%s__%s", flowName, actionName)
}

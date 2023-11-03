package action

import (
	"context"
	"fmt"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/lweeprojactionfile"
	"github.com/lefinal/lwee/lwee/lweestream"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
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

func (factory *Factory) newProjectAction(base *Base, projectActionDetails lweeflowfile.ActionRunnerProjectAction) (action, error) {
	// Assure action exists.
	actionDir := factory.Locator.ProjectActionDirByAction(projectActionDetails.Name)
	_, err := os.Stat(actionDir)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "stat action dir", meh.Details{"action_dir": actionDir})
	}
	actionLocator := factory.Locator.ProjectActionLocatorByAction(projectActionDetails.Name)
	// Read action.
	projectActionFile, err := lweeprojactionfile.FromFile(actionLocator.ActionFilename())
	if err != nil {
		return nil, meh.Wrap(err, "project action file from file",
			meh.Details{"filename": actionLocator.ActionFilename()})
	}
	if projectActionDetails.Config == "" {
		projectActionDetails.Config = "default"
	}
	actionConfig, ok := projectActionFile.Configs[projectActionDetails.Config]
	if !ok {
		knownConfigs := make([]string, 0)
		for configName := range projectActionFile.Configs {
			knownConfigs = append(knownConfigs, configName)
		}
		return nil, meh.NewBadInputErr(fmt.Sprintf("unknown action config: %s", projectActionDetails.Config),
			meh.Details{"known_configs": knownConfigs})
	}
	// Create actual action.
	switch actionConfig := actionConfig.(type) {
	case lweeprojactionfile.ProjActionConfigImage:
		if actionConfig.File == "" {
			actionConfig.File = "Dockerfile"
		}
		projectActionImage := &projectActionImage{
			Base:                  base,
			containerEngine:       factory.ContainerEngine,
			contextDir:            actionDir,
			file:                  actionConfig.File,
			tag:                   projectActionImageTag(factory.FlowName, projectActionDetails.Name),
			args:                  projectActionDetails.Args,
			containerWorkspaceDir: path.Join(factory.Locator.ActionTempDirByAction(base.actionName), "container-workspace"),
			containerState:        containerStateReady,
			containerRunningCond:  sync.NewCond(&sync.Mutex{}),
			streamConnector:       lweestream.NewConnector(base.logger.Named("stream-connector")),
		}
		err := os.MkdirAll(projectActionImage.containerWorkspaceDir, 0750)
		if err != nil {
			return nil, meh.NewInternalErrFromErr(err, "create container workspace dir",
				meh.Details{"dir": projectActionImage.containerWorkspaceDir})
		}
		return projectActionImage, nil
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
	streamConnector      lweestream.Connector
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
	stdinInputRegistered := false
	for inputName, input := range action.fileActionInputs {
		var inputRequest inputIngestionRequestWithIngestor
		switch input := input.(type) {
		case lweeflowfile.ActionInputContainerWorkspaceFile:
			inputRequest = action.newContainerWorkspaceFileInputRequest(input)
		case lweeflowfile.ActionInputStdin:
			// Assure only one input with stdin-type.
			if stdinInputRegistered {
				return meh.NewBadInputErr("duplicate stdin inputs. only one is allowed", nil)
			}
			stdinInputRegistered = true
			inputRequest = action.newStdinInputRequest(input)
		case lweeflowfile.ActionInputStream:
			var err error
			inputRequest, err = action.newStreamInputRequest(input)
			if err != nil {
				return meh.Wrap(err, "new stream input request", meh.Details{"input_name": inputName})
			}
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
		case lweeflowfile.ActionOutputStream:
			var err error
			outputOffer, err = action.newStreamOutputOffer(output)
			if err != nil {
				return meh.Wrap(err, "new stream output offer", meh.Details{"output_name": outputName})
			}
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action output %s has unsupported type: %s", outputName, output.Type()), nil)
		}
		outputOffer.offer.OutputName = outputName
		action.outputOffersByOutputName[outputName] = outputOffer
	}
	return nil
}

func (action *projectActionImage) newContainerWorkspaceFileInputRequest(input lweeflowfile.ActionInputContainerWorkspaceFile) inputIngestionRequestWithIngestor {
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhasePreStart,
			SourceName:              input.Source,
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

func (action *projectActionImage) newStdinInputRequest(input lweeflowfile.ActionInputStdin) inputIngestionRequestWithIngestor {
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhaseRunning,
			SourceName:              input.Source,
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			// Wait for container running.
			err := action.waitForContainerState(ctx, containerStateRunning)
			if err != nil {
				return meh.Wrap(err, "wait for container running", nil)
			}
			// Pipe to stdin.
			action.logger.Debug("pipe input to container stdin", zap.String("source_name", input.Source))
			containerStdin, err := action.containerEngine.ContainerStdin(ctx, action.containerID)
			if err != nil {
				return meh.Wrap(err, "open container stdin", meh.Details{"container_id": action.containerID})
			}
			defer func() { _ = containerStdin.Close() }()
			start := time.Now()
			n, err := io.Copy(containerStdin, source)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "copy to stdin", nil)
			}
			err = containerStdin.Close()
			if err != nil {
				return meh.NewInternalErrFromErr(err, "close container stdin", nil)
			}
			action.logger.Debug("completed piping input to container stdin",
				zap.Duration("took", time.Since(start)),
				zap.String("bytes_copied", logging.FormatByteCountDecimal(n)))
			return nil
		},
	}
}

func (action *projectActionImage) newStreamInputRequest(input lweeflowfile.ActionInputStream) (inputIngestionRequestWithIngestor, error) {
	err := action.streamConnector.RegisterStreamInputOffer(input.StreamName)
	if err != nil {
		return inputIngestionRequestWithIngestor{}, meh.Wrap(err, "register stream input offer at connector",
			meh.Details{"stream_name": input.StreamName})
	}
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhaseRunning,
			SourceName:              input.Source,
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			err := action.waitForContainerState(ctx, containerStateRunning)
			if err != nil {
				return meh.Wrap(err, "wait for container running", nil)
			}
			err = action.streamConnector.WriteInputStream(ctx, input.StreamName, source)
			if err != nil {
				return meh.Wrap(err, "write input stream with connector", meh.Details{"stream_name": input.StreamName})
			}
			return nil
		},
	}, nil
}

func (action *projectActionImage) waitForContainerState(ctx context.Context, state containerState) error {
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
			RequireFinishUntilPhase: PhaseStopped,
		},
		output: func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for container stopped.
			err := action.waitForContainerState(ctx, containerStateDone)
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
			RequireFinishUntilPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for container running.
			err := action.waitForContainerState(ctx, containerStateRunning)
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

func (action *projectActionImage) newStreamOutputOffer(output lweeflowfile.ActionOutputStream) (OutputOfferWithOutputter, error) {
	err := action.streamConnector.RegisterStreamOutputRequest(output.StreamName)
	if err != nil {
		return OutputOfferWithOutputter{}, meh.Wrap(err, "register stream output request at connector", meh.Details{"stream_name": output.StreamName})
	}
	return OutputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			err := action.streamConnector.ReadOutputStream(ctx, output.StreamName, ready, writer)
			if err != nil {
				return meh.Wrap(err, "read output stream with connector", meh.Details{"stream_name": output.StreamName})
			}
			return nil
		},
	}, nil
}

func (action *projectActionImage) setContainerState(newState containerState) {
	action.containerRunningCond.L.Lock()
	action.containerState = newState
	action.containerRunningCond.L.Unlock()
	action.containerRunningCond.Broadcast()
}

func (action *projectActionImage) Start(ctx context.Context) (<-chan error, error) {
	var err error
	// Create and start the container.
	containerConfig := container.Config{
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
	containerIP, err := action.containerEngine.ContainerIP(ctx, action.containerID)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "get container ip", meh.Details{"container_id": action.containerID})
	}
	// Wait until streams ready.
	err = action.streamConnector.ConnectAndVerify(ctx, containerIP, lweestream.DefaultTargetPort)
	if err != nil {
		return nil, meh.Wrap(err, "connect stream connector and verify", meh.Details{
			"target_host": containerIP,
			"target_port": lweestream.DefaultTargetPort,
		})
	}
	action.setContainerState(containerStateRunning)
	stopped := make(chan error)
	eg, _ := errgroup.WithContext(ctx)
	// Wait until the container has stopped.
	eg.Go(func() error {
		defer func() { _ = action.containerEngine.RemoveContainer(ctx, action.containerID) }()
		err := action.containerEngine.WaitForContainerStopped(ctx, action.containerID)
		action.setContainerState(containerStateDone)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "wait for container stopped", meh.Details{"container_id": action.containerID})
		}
		return nil
	})
	eg.Go(func() error {
		err := action.streamConnector.PipeIO(ctx)
		if err != nil {
			return meh.Wrap(err, "pipe io with stream connector", nil)
		}
		return nil
	})

	go func() {
		select {
		case <-ctx.Done():
		case stopped <- eg.Wait():
		}
	}()
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

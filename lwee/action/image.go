package action

import (
	"context"
	"fmt"
	"github.com/docker/go-connections/nat"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/lweestream"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"path/filepath"
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

// imageRunner runs a container image.
type imageRunner struct {
	*base
	containerEngine   container.Engine
	imageTag          string
	command           []string
	env               map[string]string
	workspaceHostDir  string
	workspaceMountDir string
	containerID       string
	// containerState is the state of the container. It is locked using
	// containerRunningCond.
	containerState containerState
	// containerRunningCond locks containerState.
	containerRunningCond *sync.Cond
	streamConnector      lweestream.Connector
}

func (action *imageRunner) Verify(_ context.Context) error {
	return nil
}

func (action *imageRunner) registerInputIngestionRequests() error {
	stdinInputRegistered := false
	for inputName, input := range action.fileActionInputs {
		var inputRequest inputIngestionRequestWithIngestor
		switch input := input.(type) {
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
		case lweeflowfile.ActionInputWorkspaceFile:
			var err error
			inputRequest, err = action.newWorkspaceFileInputRequest(input)
			if err != nil {
				return meh.Wrap(err, "new workspace file input request", meh.Details{"input_name": inputName})
			}
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action input %q has unsupported type: %s", inputName, input.Type()), nil)
		}
		inputRequest.request.InputName = inputName
		action.inputIngestionRequestsByInputName[inputName] = inputRequest
	}
	return nil
}

func (action *imageRunner) registerOutputProviders() error {
	stdoutOutputRegistered := false
	for outputName, output := range action.fileActionOutputs {
		var outputOffer outputOfferWithOutputter
		switch output := output.(type) {
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
		case lweeflowfile.ActionOutputWorkspaceFile:
			var err error
			outputOffer, err = action.newWorkspaceFileOutputOffer(output)
			if err != nil {
				return meh.Wrap(err, "new workspace file output offer", meh.Details{"output_name": outputName})
			}
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action output %s has unsupported type: %s", outputName, output.Type()), nil)
		}
		outputOffer.offer.OutputName = outputName
		action.outputOffersByOutputName[outputName] = outputOffer
	}
	return nil
}

func (action *imageRunner) newStdinInputRequest(input lweeflowfile.ActionInputStdin) inputIngestionRequestWithIngestor {
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

func (action *imageRunner) newStreamInputRequest(input lweeflowfile.ActionInputStream) (inputIngestionRequestWithIngestor, error) {
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

func (action *imageRunner) newWorkspaceFileInputRequest(input lweeflowfile.ActionInputWorkspaceFile) (inputIngestionRequestWithIngestor, error) {
	if filepath.IsAbs(input.Filename) {
		return inputIngestionRequestWithIngestor{}, meh.NewBadInputErr("filename must not be absolute", meh.Details{"filename": input.Filename})
	}
	filename := filepath.Join(action.workspaceHostDir, input.Filename)
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhasePreStart,
			SourceName:              input.Source,
		},
		optimize: func(ctx context.Context, availableOptimizations *actionio.AvailableOptimizations) {
			// If a file is available, we simply move it.
			if availableOptimizations.Filename != "" {
				action.logger.Debug("filename optimization available. moving file.",
					zap.String("src", availableOptimizations.Filename),
					zap.String("dst", filename))
				err := actionio.ApplyFileCopyOptimization(availableOptimizations.Filename, filename)
				if err != nil {
					mehlog.LogToLevel(action.logger, zap.DebugLevel, meh.Wrap(err, "move file", meh.Details{
						"src": availableOptimizations.Filename,
						"dst": filename,
					}))
					action.logger.Debug("copy source regularly due to failed move")
					return
				}
				availableOptimizations.SkipTransmission()
			}
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			err := os.MkdirAll(filepath.Dir(filename), 0750)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "mkdir all", meh.Details{"dir": filepath.Dir(filename)})
			}
			f, err := os.Create(filename)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "create workspace file", meh.Details{"filename": filename})
			}
			defer func() { _ = f.Close() }()
			_, err = io.Copy(f, source)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "write workspace file", meh.Details{"filename": filename})
			}
			err = f.Close()
			if err != nil {
				return meh.NewInternalErrFromErr(err, "close written workspace file", meh.Details{"filename": filename})
			}
			return nil
		},
	}, nil
}

func (action *imageRunner) waitForContainerState(ctx context.Context, state containerState) error {
	action.containerRunningCond.L.Lock()
	for {
		if action.containerState >= state {
			break
		}
		select {
		case <-ctx.Done():
			return meh.NewBadInputErrFromErr(ctx.Err(), "context done while waiting for container state",
				meh.Details{"wait_for_state": state})
		default:
		}
		action.containerRunningCond.Wait()
	}
	action.containerRunningCond.L.Unlock()
	return nil
}

func (action *imageRunner) newStdoutOutputOffer() outputOfferWithOutputter {
	return outputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- actionio.AlternativeSourceAccess, writer io.WriteCloser) error {
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
			case ready <- actionio.AlternativeSourceAccess{}:
			}
			// Forward.
			_, err = io.Copy(writer, stdoutReader)
			if err != nil {
				return meh.Wrap(err, "copy stdout to output writer", nil)
			}
			action.logger.Debug("read stdout logs done")
			return nil
		},
	}
}

func (action *imageRunner) newStreamOutputOffer(output lweeflowfile.ActionOutputStream) (outputOfferWithOutputter, error) {
	err := action.streamConnector.RegisterStreamOutputRequest(output.StreamName)
	if err != nil {
		return outputOfferWithOutputter{}, meh.Wrap(err, "register stream output request at connector", meh.Details{"stream_name": output.StreamName})
	}
	return outputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- actionio.AlternativeSourceAccess, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			err := action.streamConnector.ReadOutputStream(ctx, output.StreamName, ready, writer)
			if err != nil {
				return meh.Wrap(err, "read output stream with connector", meh.Details{"stream_name": output.StreamName})
			}
			return nil
		},
	}, nil
}

func (action *imageRunner) newWorkspaceFileOutputOffer(output lweeflowfile.ActionOutputWorkspaceFile) (outputOfferWithOutputter, error) {
	if filepath.IsAbs(output.Filename) {
		return outputOfferWithOutputter{}, meh.NewBadInputErr("filename must not be absolute", meh.Details{"filename": output.Filename})
	}
	filename := filepath.Join(action.workspaceHostDir, output.Filename)
	return outputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseStopped,
		},
		output: func(ctx context.Context, ready chan<- actionio.AlternativeSourceAccess, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for container stopped.
			err := action.waitForContainerState(ctx, containerStateDone)
			if err != nil {
				return meh.Wrap(err, "wait for container done", nil)
			}
			action.logger.Debug("container done. now providing workspace file output.",
				zap.String("filename", filename))
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "notify output ready", nil)
			case ready <- actionio.AlternativeSourceAccess{Filename: filename}:
			}
			// Copy file.
			f, err := os.Open(filename)
			if err != nil {
				return meh.NewBadInputErrFromErr(err, "open workspace output file",
					meh.Details{"filename": filename})
			}
			defer func() { _ = f.Close() }()
			_, err = io.Copy(writer, f)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "copy workspace output file",
					meh.Details{"filename": filename})
			}
			return nil
		},
	}, nil
}

func (action *imageRunner) setContainerState(newState containerState) {
	action.containerRunningCond.L.Lock()
	action.containerState = newState
	action.containerRunningCond.L.Unlock()
	action.containerRunningCond.Broadcast()
}

func (action *imageRunner) RunInfo(_ context.Context) (map[string]string, error) {
	return nil, nil
}

func (action *imageRunner) Start(ctx context.Context) (<-chan error, error) {
	var err error
	// Create and start the container.
	sdkPort, err := nat.NewPort("tcp", lweestream.DefaultTargetPort)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create default lwee stream target port",
			meh.Details{"port": lweestream.DefaultTargetPort})
	}
	containerConfig := container.Config{
		ExposedPorts: map[nat.Port]struct{}{
			sdkPort: {},
		},
		VolumeMounts: []container.VolumeMount{
			{
				Source: action.workspaceHostDir,
				Target: action.workspaceMountDir,
			},
		},
		Command: action.command,
		Env:     action.env,
		Image:   action.imageTag,
	}
	action.logger.Debug("create container",
		zap.String("image", action.imageTag),
		zap.Any("volumes", containerConfig.VolumeMounts))
	action.containerID, err = action.containerEngine.CreateContainer(ctx, containerConfig)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create container", meh.Details{"container_config": containerConfig})
	}
	err = action.containerEngine.StartContainer(ctx, action.containerID)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "start container", meh.Details{"container_id": action.containerID})
	}
	if action.streamConnector.HasRegisteredIO() {
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
	}
	action.setContainerState(containerStateRunning)
	stopped := make(chan error)
	startCtx := ctx
	eg, ctx := errgroup.WithContext(ctx)
	// Wait until the container has stopped.
	eg.Go(func() error {
		defer func() { _ = action.containerEngine.RemoveContainer(context.Background(), action.containerID) }()
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
		case <-startCtx.Done():
		case stopped <- eg.Wait():
		}
	}()
	return stopped, nil
}

func (action *imageRunner) Stop(ctx context.Context) error {
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
	const replaceNonAlphanumericWith = 'x'
	flowName = locator.ToAlphanumeric(flowName, replaceNonAlphanumericWith)
	flowName = strings.ToLower(flowName)
	actionName = locator.ToAlphanumeric(actionName, replaceNonAlphanumericWith)
	actionName = strings.ToLower(actionName)
	return fmt.Sprintf("lwee_%s_%s", flowName, actionName)
}

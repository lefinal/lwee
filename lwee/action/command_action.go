package action

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/commandassert"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/runinfo"
	"github.com/lefinal/lwee/lwee/templaterender"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"time"
)

type commandState int

const (
	commandStateReady commandState = iota
	commandStateStarted
	commandStateRunning
	commandStateDone
)

type commandActionExtraRenderData struct {
	WorkspaceDir string
}

type commandAction struct {
	*Base
	assertions       map[string]commandassert.Assertion
	runInfoProviders map[string]runinfo.Provider
	command          []string
	workspaceDir     string
	// stdinReader is not nil when an input ingestion request for stdin was made.
	// Input data will be available via this reader.
	stdinReader io.Reader
	// stdoutWriter is not nil when an output offer for stdout was made. The
	// command's output data will be written to this writer.
	stdoutWriter io.Writer
	// cmd is the actual command to run.
	cmd *exec.Cmd
	// cancelCmd is a cancel-func that will be initialized in Start method. A call to
	// Stop cancels the commands run context using cancelCmd.
	cancelCmd context.CancelFunc
	// commandState is the state of the container, locked using commandStateCond.
	commandState commandState
	// commandStateCond locks commandState.
	commandStateCond *sync.Cond
}

func (factory *Factory) newCommandAction(base *Base, renderData templaterender.Data, commandActionDetails lweeflowfile.ActionRunnerCommand) (action, error) {
	// Render action runner details.
	workspaceDir := factory.Locator.ActionWorkspaceDirByAction(base.actionName)
	renderData.Action.Extras = commandActionExtraRenderData{
		WorkspaceDir: workspaceDir,
	}
	renderer := templaterender.New(renderData)
	err := commandActionDetails.Render(renderer)
	if err != nil {
		return nil, meh.Wrap(err, "render command action details", nil)
	}
	if len(commandActionDetails.Command) == 0 || commandActionDetails.Command[0] == "" {
		return nil, meh.NewBadInputErr("missing command", nil)
	}
	// Build the actual action.
	commandAction := &commandAction{
		Base:             base,
		assertions:       make(map[string]commandassert.Assertion),
		runInfoProviders: make(map[string]runinfo.Provider),
		command:          commandActionDetails.Command,
		workspaceDir:     workspaceDir,
		commandState:     commandStateReady,
		commandStateCond: sync.NewCond(&sync.Mutex{}),
	}
	err = os.MkdirAll(commandAction.workspaceDir, 0750)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create workspace dir",
			meh.Details{"dir": commandAction.workspaceDir})
	}
	// Create assertions.
	for assertionName, assertionOptions := range commandActionDetails.Assertions {
		assertion, err := commandassert.New(commandassert.Options{
			Logger: base.logger.Named("assertion").Named(logging.WrapName(assertionName)),
			Run:    assertionOptions.Run,
			Should: commandassert.ShouldType(assertionOptions.Should),
			Target: assertionOptions.Target,
		})
		if err != nil {
			return nil, meh.Wrap(err, "build assertion", meh.Details{
				"assertion_name":    assertionName,
				"assertion_options": assertionOptions,
			})
		}
		commandAction.assertions[assertionName] = assertion
	}
	// Create run info providers.
	for infoName, infoDetails := range commandActionDetails.RunInfo {
		provider, err := runinfo.NewProvider(infoDetails)
		if err != nil {
			return nil, meh.Wrap(err, fmt.Sprintf("new run info provider for %q", infoName), meh.Details{"info_details": infoDetails})
		}
		commandAction.runInfoProviders[infoName] = provider
	}
	return commandAction, nil
}

func (action *commandAction) Verify(ctx context.Context) error {
	for assertionName, assertion := range action.assertions {
		err := assertion.Assert(ctx)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("assert %q", assertionName), nil)
		}
	}
	return nil
}

// Build assures that the command exists.
func (action *commandAction) Build(_ context.Context) error {
	filename, err := exec.LookPath(action.command[0])
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "look up command", meh.Details{"command": action.command})
	}
	action.logger.Debug("command lookup succeeded",
		zap.String("command", action.command[0]),
		zap.String("command_filename", filename))
	return nil
}

func (action *commandAction) registerInputIngestionRequests() error {
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
		case lweeflowfile.ActionInputWorkspaceFile:
			inputRequest = action.newWorkspaceFileInputRequest(input)
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action input %q has unsupported type: %s", inputName, input.Type()), nil)
		}
		inputRequest.request.InputName = inputName
		action.inputIngestionRequestsByInputName[inputName] = inputRequest
	}
	return nil
}

// newStdinInputRequest creates a new input request for stdin. It sets the
// stdinReader that the action will write to, once the source is available.
func (action *commandAction) newStdinInputRequest(input lweeflowfile.ActionInputStdin) inputIngestionRequestWithIngestor {
	stdinReader, stdinWriter := io.Pipe()
	action.stdinReader = stdinReader

	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhaseRunning,
			SourceName:              input.Source,
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			defer func() { _ = stdinWriter.Close() }()
			// Pipe to stdin.
			start := time.Now()
			action.logger.Debug("pipe input to command stdin", zap.String("source_name", input.Source))
			n, err := io.Copy(stdinWriter, source)
			if err != nil {
				return meh.Wrap(err, "copy to stdin", nil)
			}
			err = stdinWriter.Close()
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

func (action *commandAction) newWorkspaceFileInputRequest(input lweeflowfile.ActionInputWorkspaceFile) inputIngestionRequestWithIngestor {
	return inputIngestionRequestWithIngestor{
		request: InputIngestionRequest{
			RequireFinishUntilPhase: PhasePreStart,
			SourceName:              input.Source,
		},
		ingest: func(ctx context.Context, source io.Reader) error {
			filename := input.Filename
			if !filepath.IsAbs(filename) {
				filename = path.Join(action.workspaceDir, input.Filename)
			}
			err := os.MkdirAll(path.Dir(filename), 0750)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "mkdir all", meh.Details{"dir": path.Dir(filename)})
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
	}
}

func (action *commandAction) registerOutputProviders() error {
	stdoutOutputRegistered := false
	for outputName, output := range action.fileActionOutputs {
		var outputOffer OutputOfferWithOutputter
		switch output := output.(type) {

		case lweeflowfile.ActionOutputStdout:
			// Assure only one output with stdout-type.
			if stdoutOutputRegistered {
				return meh.NewBadInputErr("duplicate stdout outputs. only one is allowed.", nil)
			}
			stdoutOutputRegistered = true
			outputOffer = action.newStdoutOutputOffer()
		case lweeflowfile.ActionOutputWorkspaceFile:
			outputOffer = action.newWorkspaceFileOutputOffer(output)
		default:
			return meh.NewBadInputErr(fmt.Sprintf("action output %s has unsupported type: %s", outputName, output.Type()), nil)
		}
		outputOffer.offer.OutputName = outputName
		action.outputOffersByOutputName[outputName] = outputOffer
	}
	return nil
}

func (action *commandAction) newStdoutOutputOffer() OutputOfferWithOutputter {
	stdoutReader, stdoutWriter := io.Pipe()
	action.stdoutWriter = stdoutWriter

	return OutputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseRunning,
		},
		output: func(ctx context.Context, ready chan<- actionio.AlternativeSourceAccess, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			defer func() { _ = stdoutWriter.Close() }()
			// Wait for command running.
			err := action.waitForCommandState(ctx, commandStateRunning)
			if err != nil {
				return meh.Wrap(err, "wait for command running", nil)
			}
			// The writer does not get closed by the command, so we need to listen for
			// command state changes in order to close it manually. We can rely on the
			// command state "done" to have finished writing all stdout data to the writer as
			// the Wait-method on exec.Command only returns once all data has been written to
			// stdout.
			go func() {
				defer func() { _ = stdoutWriter.Close() }()
				_ = action.waitForCommandState(ctx, commandStateDone)
			}()
			// Notify output ready.
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "notify output open", nil)
			case ready <- actionio.AlternativeSourceAccess{}:
			}
			// Forward.
			start := time.Now()
			n, err := io.Copy(writer, stdoutReader)
			if err != nil {
				return meh.Wrap(err, "copy stdout logs", nil)
			}
			action.logger.Debug("read stdout logs done",
				zap.Int64("bytes_copied", n),
				zap.Duration("took", time.Since(start)))
			return nil
		},
	}
}

func (action *commandAction) newWorkspaceFileOutputOffer(output lweeflowfile.ActionOutputWorkspaceFile) OutputOfferWithOutputter {
	return OutputOfferWithOutputter{
		offer: OutputOffer{
			RequireFinishUntilPhase: PhaseStopped,
		},
		output: func(ctx context.Context, ready chan<- actionio.AlternativeSourceAccess, writer io.WriteCloser) error {
			defer func() { _ = writer.Close() }()
			// Wait for command stopped.
			err := action.waitForCommandState(ctx, commandStateDone)
			if err != nil {
				return meh.Wrap(err, "wait for command done", nil)
			}
			filename := path.Join(action.workspaceDir, output.Filename)
			action.logger.Debug("command done. now providing workspace file output.",
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
			start := time.Now()
			n, err := io.Copy(writer, f)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "copy workspace output file",
					meh.Details{"filename": filename})
			}
			action.logger.Debug("copied workspace output file",
				zap.String("filename", filename),
				zap.Int64("bytes_written", n),
				zap.Duration("took", time.Since(start)))
			return nil
		},
	}
}

func (action *commandAction) RunInfo(ctx context.Context) (map[string]string, error) {
	runInfo, err := runinfo.EvalAll(ctx, action.runInfoProviders)
	if err != nil {
		return nil, meh.Wrap(err, "eval all run info providers", nil)
	}
	return runInfo, nil
}

// Start creates the actual exec.Command and starts it. The returned chanel
// receives the resulting error (or nil) once the action, including the command,
// has completed.
func (action *commandAction) Start(startCtx context.Context) (<-chan error, error) {
	ctx, cancel := context.WithCancel(startCtx)
	action.cancelCmd = cancel
	eg, ctx := errgroup.WithContext(ctx)
	// Prepare command.
	action.cmd = exec.CommandContext(ctx, action.command[0], action.command[1:]...)
	action.cmd.Dir = action.workspaceDir
	if action.stdinReader != nil {
		action.cmd.Stdin = action.stdinReader
	}
	if action.stdoutWriter != nil {
		action.cmd.Stdout = action.stdoutWriter
	}
	// Start the command.
	action.logger.Debug("start command", zap.String("command_description", action.cmd.String()))
	start := time.Now()
	err := action.cmd.Start()
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "start command", meh.Details{"command": action.cmd.String()})
	}
	action.setCommandState(commandStateStarted)
	stopped := make(chan error)
	// We currently do not support streams for commands, so we don't have to wait
	// until they are available.
	action.setCommandState(commandStateRunning)
	eg.Go(func() error {
		defer action.setCommandState(commandStateDone)
		err := action.cmd.Wait()
		if err != nil {
			return meh.NewInternalErrFromErr(err, "wait for command", meh.Details{"command": action.cmd.String()})
		}
		action.logger.Debug("command finished", zap.Duration("took", time.Since(start)))
		return nil
	})

	go func() {
		defer cancel()
		select {
		case <-startCtx.Done():
		case stopped <- eg.Wait():
		}
	}()
	return stopped, nil
}

func (action *commandAction) Stop(_ context.Context) error {
	action.cancelCmd()
	return nil
}

func (action *commandAction) setCommandState(newState commandState) {
	action.commandStateCond.L.Lock()
	action.commandState = newState
	action.commandStateCond.L.Unlock()
	action.commandStateCond.Broadcast()
}

func (action *commandAction) waitForCommandState(ctx context.Context, state commandState) error {
	action.commandStateCond.L.Lock()
	for {
		if action.commandState >= state {
			break
		}
		select {
		case <-ctx.Done():
			return meh.NewBadInputErrFromErr(ctx.Err(), "context done while waiting for command state",
				meh.Details{"wait_for_state": state})
		default:
		}
		action.commandStateCond.Wait()
	}
	action.commandStateCond.L.Unlock()
	return nil
}

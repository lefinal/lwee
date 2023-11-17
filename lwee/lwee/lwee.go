package lwee

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/action"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/runinfo"
	scheduler "github.com/lefinal/lwee/lwee/scheduler"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net"
	"os"
	"path"
	"sync"
	"time"
)

type Config struct {
	VerifyOnly          bool
	KeepTemporaryFiles  bool
	ContainerEngineType container.EngineType
}

type LWEE struct {
	logger          *zap.Logger
	config          Config
	actions         []action.Action
	flowFile        lweeflowfile.Flow
	Locator         *locator.Locator
	ioSupplier      actionio.Supplier
	runInfoRecorder *runinfo.Recorder
}

type flowOutput func(ctx context.Context) error

func New(logger *zap.Logger, flowFile lweeflowfile.Flow, locator *locator.Locator, config Config) (*LWEE, error) {
	// Verify the flow.
	report := flowFile.Validate()
	for _, issue := range report.Errors {
		logger.Error(fmt.Sprintf("invalid flow field: %s: %s", issue.Field, issue.Detail),
			zap.String("field", issue.Field),
			zap.Any("bad_value", issue.BadValue),
			zap.String("detail", issue.Detail))
	}
	for _, issue := range report.Warnings {
		logger.Warn(fmt.Sprintf("flow issue: %s", issue.Detail),
			zap.String("field", issue.Field),
			zap.Any("bad_value", issue.BadValue))
	}
	if len(report.Errors) > 0 {
		return nil, meh.NewBadInputErr("invalid flow", meh.Details{
			"errors":   len(report.Errors),
			"warnings": len(report.Warnings),
		})
	}
	// Set up LWEE.
	runInfoRecorder := runinfo.NewCollector(logger.Named("run-info"))
	lwee := &LWEE{
		logger:          logger,
		config:          config,
		Locator:         locator,
		actions:         make([]action.Action, 0),
		flowFile:        flowFile,
		ioSupplier:      actionio.NewSupplier(logger.Named("io"), actionio.CopyOptions{}, runInfoRecorder),
		runInfoRecorder: runInfoRecorder,
	}
	return lwee, nil
}

func (lwee *LWEE) Run(ctx context.Context) error {
	lwee.runInfoRecorder.RecordFlowName(lwee.flowFile.Name)
	// Setup container engine.
	containerEngine, err := container.NewEngine(ctx, lwee.logger.Named("container-engine"), lwee.config.ContainerEngineType)
	if err != nil {
		return meh.Wrap(err, "new container engine", meh.Details{"engine_type": lwee.config.ContainerEngineType})
	}
	err = containerEngine.Start(ctx)
	if err != nil {
		return meh.Wrap(err, "start container engine", nil)
	}
	defer containerEngine.Stop()
	// Create actions.
	actionFactory := &action.Factory{
		FlowName:        lwee.flowFile.Name,
		RawFlow:         lwee.flowFile.Raw,
		Locator:         lwee.Locator,
		ContainerEngine: containerEngine,
		IOSupplier:      lwee.ioSupplier,
	}
	for actionName, actionFile := range lwee.flowFile.Actions {
		newAction, err := actionFactory.NewAction(lwee.logger.Named("action").Named(logging.WrapName(actionName)), actionName, actionFile)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("create action %q", actionName), meh.Details{"action_name": actionName})
		}
		lwee.actions = append(lwee.actions, newAction)
	}
	lwee.logger.Info(fmt.Sprintf("loaded %d action(s)", len(lwee.actions)))
	// Verify all actions.
	lwee.logger.Info("verify actions")
	err = lwee.verifyActions(ctx)
	if err != nil {
		return meh.Wrap(err, "verify actions", nil)
	}
	// Build all actions.
	lwee.logger.Info("build actions")
	err = lwee.buildActions(ctx)
	if err != nil {
		return meh.Wrap(err, "build actions", nil)
	}
	lwee.logger.Info("register io")
	// Register flow inputs.
	for inputName, flowInput := range lwee.flowFile.Inputs {
		err = lwee.registerFlowInput(ctx, inputName, flowInput)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("register flow input %q", inputName), nil)
		}
	}
	// Register flow outputs.
	flowOutputs := make([]flowOutput, 0)
	for outputName, flowOutput := range lwee.flowFile.Outputs {
		registeredFlowOutput, err := lwee.registerFlowOutput(ctx, outputName, flowOutput)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("register flow output %q", outputName), nil)
		}
		flowOutputs = append(flowOutputs, registeredFlowOutput)
	}
	// Prepare scheduler and register IO.
	eg, runCtx := errgroup.WithContext(ctx)
	actionScheduler, err := scheduler.New(runCtx, lwee.logger.Named("scheduler"), lwee.ioSupplier, lwee.actions, lwee.runInfoRecorder)
	if err != nil {
		return meh.Wrap(err, "setup actions with scheduler", nil)
	}
	err = lwee.ioSupplier.Validate()
	if err != nil {
		return meh.Wrap(err, "validate io", nil)
	}
	lwee.logger.Debug("action io configuration is valid")
	if lwee.config.VerifyOnly {
		return nil
	}
	// Defer cleanup.
	defer func() {
		if lwee.config.KeepTemporaryFiles {
			lwee.logger.Warn(fmt.Sprintf("keeping temporary files in %s", lwee.Locator.ActionTempDir()))
		} else {
			lwee.logger.Debug("delete temporary files", zap.String("action_temp_dir", lwee.Locator.ActionTempDir()))
			_ = os.RemoveAll(lwee.Locator.ActionTempDir())
		}
	}()
	// Log run info.
	err = lwee.logActionRunInfo(ctx)
	if err != nil {
		return meh.Wrap(err, "log action run info", nil)
	}
	// Run.
	start := time.Now()
	lwee.runInfoRecorder.RecordFlowStart(start)
	lwee.logger.Info("run actions")
	eg.Go(func() error {
		err := actionScheduler.Run()
		if err != nil {
			return meh.Wrap(err, "schedule", nil)
		}
		return nil
	})
	eg.Go(func() error {
		err := lwee.ioSupplier.Forward(runCtx)
		if err != nil {
			return meh.Wrap(err, "forward io", nil)
		}
		return nil
	})
	for _, flowOutput := range flowOutputs {
		flowOutput := flowOutput
		eg.Go(func() error {
			return flowOutput(runCtx)
		})
	}
	err = eg.Wait()
	if err != nil {
		return meh.Wrap(err, "run actions", nil)
	}
	lwee.runInfoRecorder.RecordFlowEnd(time.Now())
	lwee.logger.Info("done", zap.Duration("took", time.Since(start)))
	// Write run info results.
	err = lwee.writeRunInfoResult()
	if err != nil {
		return meh.Wrap(err, "write run info result", nil)
	}
	return nil
}

// verifyActions verifies all actions.
func (lwee *LWEE) verifyActions(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, actionToVerify := range lwee.actions {
		actionToVerify := actionToVerify
		eg.Go(func() error {
			err := actionToVerify.Verify(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("verify action %q", actionToVerify.Name()), nil)
			}
			return nil
		})
	}
	return eg.Wait()
}

// buildActions builds all actions.
func (lwee *LWEE) buildActions(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	actionsBuilt := 0
	var actionsBuiltMutex sync.Mutex
	for _, actionToBuild := range lwee.actions {
		actionToBuild := actionToBuild
		eg.Go(func() error {
			start := time.Now()
			err := actionToBuild.Build(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("build action %q", actionToBuild.Name()), nil)
			}
			actionsBuiltMutex.Lock()
			actionsBuilt++
			lwee.logger.Debug(fmt.Sprintf("finished action build %d/%d", actionsBuilt, len(lwee.actions)),
				zap.String("action_name", actionToBuild.Name()),
				zap.Duration("took", time.Since(start)))
			actionsBuiltMutex.Unlock()
			return nil
		})
	}
	return eg.Wait()
}

// logActionRunInfo evaluates run information for all action and saves it to an
// output file for improved reproducibility.
func (lwee *LWEE) logActionRunInfo(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, actionToHandle := range lwee.actions {
		actionToHandle := actionToHandle
		eg.Go(func() error {
			runInfo, err := actionToHandle.RunInfo(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("run log info for action %q", actionToHandle.Name()), nil)
			}
			lwee.runInfoRecorder.RecordActionInfo(actionToHandle.Name(), runInfo)
			return nil
		})
	}
	return eg.Wait()
}

func (lwee *LWEE) writeRunInfoResult() error {
	runInfoResult, err := lwee.runInfoRecorder.Result()
	if err != nil {
		return meh.Wrap(err, "get run info result", nil)
	}
	filename := lwee.Locator.RunInfoYAMLFilename()
	err = locator.CreateDirIfNotExists(path.Dir(filename))
	if err != nil {
		return meh.Wrap(err, "create dir if not exists", meh.Details{"dir": path.Dir(filename)})
	}
	err = os.WriteFile(filename, runInfoResult, 0600)
	if err != nil {
		return meh.Wrap(err, "write run info result", meh.Details{"filename": filename})
	}
	return nil
}

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, meh.NewInternalErrFromErr(err, "resolve tcp address", nil)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, meh.NewInternalErrFromErr(err, "listen on random port", nil)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

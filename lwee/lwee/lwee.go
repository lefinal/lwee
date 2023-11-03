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
	scheduler "github.com/lefinal/lwee/lwee/scheduler"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net"
	"os"
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
	containerEngine container.Engine
	actionFactory   *action.Factory
	actions         []action.Action
	flowFile        lweeflowfile.Flow
	Locator         *locator.Locator
	ioSupplier      actionio.Supplier
}

type flowOutput func(ctx context.Context) error

func New(logger *zap.Logger, flowFile lweeflowfile.Flow, locator *locator.Locator, config Config) (*LWEE, error) {
	containerEngine, err := container.NewEngine(logger.Named("container-engine"), config.ContainerEngineType)
	if err != nil {
		return nil, meh.Wrap(err, "new container engine", meh.Details{"engine_type": config.ContainerEngineType})
	}
	lwee := &LWEE{
		logger:          logger,
		config:          config,
		containerEngine: containerEngine,
		Locator:         locator,
		actions:         make([]action.Action, 0),
		flowFile:        flowFile,
		ioSupplier:      actionio.NewSupplier(logger.Named("io")),
	}
	lwee.actionFactory = &action.Factory{
		FlowName:        lwee.flowFile.Name,
		Locator:         lwee.Locator,
		ContainerEngine: lwee.containerEngine,
		IOSupplier:      lwee.ioSupplier,
	}
	return lwee, nil
}

func (lwee *LWEE) Run(ctx context.Context) error {
	// Create actions.
	var err error
	for actionName, actionFile := range lwee.flowFile.Actions {
		newAction, err := lwee.actionFactory.NewAction(lwee.logger.Named("action").Named(logging.WrapName(actionName)), actionName, actionFile)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("create action %q", actionName), meh.Details{"action_name": actionName})
		}
		lwee.actions = append(lwee.actions, newAction)
	}
	lwee.logger.Info(fmt.Sprintf("loaded %d action(s)", len(lwee.actions)))
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
	actionScheduler, err := scheduler.New(ctx, lwee.logger.Named("scheduler"), lwee.ioSupplier, lwee.actions)
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
			lwee.logger.Info(fmt.Sprintf("keeping temporary files in %s", lwee.Locator.ActionTempDir()))
		} else {
			lwee.logger.Debug("delete temporary files", zap.String("action_temp_dir", lwee.Locator.ActionTempDir()))
			_ = os.RemoveAll(lwee.Locator.ActionTempDir())
		}
	}()
	// Run.
	start := time.Now()
	lwee.logger.Info("run actions")
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		err := actionScheduler.Run()
		if err != nil {
			return meh.Wrap(err, "schedule", nil)
		}
		return nil
	})
	eg.Go(func() error {
		err := lwee.ioSupplier.Forward(ctx)
		if err != nil {
			return meh.Wrap(err, "forward io", nil)
		}
		return nil
	})
	for _, flowOutput := range flowOutputs {
		flowOutput := flowOutput
		eg.Go(func() error {
			return flowOutput(ctx)
		})
	}
	err = eg.Wait()
	if err != nil {
		return meh.Wrap(err, "run actions", nil)
	}
	lwee.logger.Info("done", zap.Duration("took", time.Since(start)))
	return nil
}

// buildActions builds all actions.
func (lwee *LWEE) buildActions(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for actionName, actionToBuild := range lwee.actions {
		actionName := actionName
		actionToBuild := actionToBuild
		eg.Go(func() error {
			err := actionToBuild.Build(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("build action %q", actionName), nil)
			}
			return nil
		})
	}
	return eg.Wait()
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

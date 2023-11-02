package lwee

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/action"
	"github.com/lefinal/lwee/container"
	"github.com/lefinal/lwee/locator"
	"github.com/lefinal/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net"
	"strconv"
)

type Config struct {
	ContainerEngineType container.EngineType
}

type LWEE struct {
	logger          *zap.Logger
	config          Config
	containerEngine container.Engine
	actionFactory   *action.Factory
	actions         map[string]action.Action
	flowFile        lweeflowfile.Flow
	Locator         *locator.Locator
}

func New(logger *zap.Logger, flowFile lweeflowfile.Flow, locator *locator.Locator, config Config) (*LWEE, error) {
	containerEngine, err := container.NewEngine(config.ContainerEngineType)
	if err != nil {
		return nil, meh.Wrap(err, "new container engine", meh.Details{"engine_type": config.ContainerEngineType})
	}
	return &LWEE{
		logger:          logger,
		config:          config,
		containerEngine: containerEngine,
		actionFactory: &action.Factory{
			FlowName:        flowFile.Name,
			Locator:         locator,
			ContainerEngine: containerEngine,
		},
		Locator:  locator,
		actions:  make(map[string]action.Action),
		flowFile: flowFile,
	}, nil
}

func (lwee *LWEE) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	// Create actions.
	var err error
	for actionName, actionFile := range lwee.flowFile.Actions {
		lwee.actions[actionName], err = lwee.actionFactory.NewAction(lwee.logger.Named("action").Named(actionName), actionFile)
		if err != nil {
			return meh.Wrap(err, "create action", meh.Details{"action_name": actionName})
		}
	}
	lwee.logger.Info(fmt.Sprintf("loaded %d action(s)", len(lwee.actions)))
	// Build all actions.
	lwee.logger.Info("build actions")
	err = lwee.buildActions(ctx)
	if err != nil {
		return meh.Wrap(err, "build actions", nil)
	}

	// TODO

	return eg.Wait()
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

func newMasterNodeListener() (net.Listener, error) {
	masterNodePort, err := getFreePort()
	if err != nil {
		return nil, meh.Wrap(err, "get free port", nil)
	}
	listenAddr := net.JoinHostPort("localhost", strconv.Itoa(masterNodePort))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "listen", meh.Details{"listen_addr": listenAddr})
	}
	return listener, nil
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

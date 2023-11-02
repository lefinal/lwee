package app

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"path"
)

const defaultFlowFilename = "flow.yaml"

type Config struct {
	EngineType container.EngineType
	Command    string
	ListenAddr string
	// ContextDir for locating files.
	ContextDir string
	// FlowFilename to use. If not set, it will be generated with default values in
	// Run.
	FlowFilename string
}

type command struct {
	noLocatorRequired bool
	run               func(ctx context.Context, logger *zap.Logger, config Config) error
}

var commands = map[string]command{
	"create-action": {
		run: commandCreateProjectAction,
	},
	"init": {
		run: commandInit,
	},
	"verify": {
		run: commandVerify,
	},
}

func Run(ctx context.Context, logger *zap.Logger, config Config) error {
	if config.ContextDir != "" && config.FlowFilename == "" {
		config.FlowFilename = path.Join(config.ContextDir, defaultFlowFilename)
	}
	// Prepare command.
	if config.Command == "" {
		return meh.NewBadInputErr("missing command", nil)
	}
	commandToRun, ok := commands[config.Command]
	if !ok {
		return meh.NewBadInputErr(fmt.Sprintf("unsupported command: %v", config.Command), nil)
	}
	// Set up engine.
	err := container.SetEngine(config.EngineType)
	if err != nil {
		return meh.Wrap(err, "set engine", meh.Details{"engine_type": config.EngineType})
	}
	// Set up locator.
	if !commandToRun.noLocatorRequired {
		if config.ContextDir == "" {
			return meh.NewBadInputErr("missing context dir", nil)
		}
		appLocator, err := locator.New(config.ContextDir, config.FlowFilename)
		if err != nil {
			return meh.Wrap(err, "new locator", meh.Details{"context_dir": config.ContextDir})
		}
		locator.SetDefault(appLocator)
	}
	err = commandToRun.run(ctx, logger, config)
	if err != nil {
		return err
	}
	return nil
}

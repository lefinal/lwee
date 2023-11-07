package app

import (
	"context"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/projactionbuilder"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

// commandCreateProjectAction creates an action directory and template for a new
// action. The user is asked for input regarding the action name.
func commandCreateProjectAction(ctx context.Context, logger *zap.Logger, input input.Input, config Config) error {
	// Create.
	containerEngine, err := container.NewEngine(ctx, logger.Named("container-engine"), config.EngineType)
	if err != nil {
		return meh.Wrap(err, "create container engine", nil)
	}
	err = containerEngine.Start(ctx)
	if err != nil {
		return meh.Wrap(err, "start container engine", nil)
	}
	defer containerEngine.Stop()
	builder := projactionbuilder.NewBuilder(logger, locator.Default(), input, containerEngine)
	err = builder.Build(ctx)
	if err != nil {
		return meh.Wrap(err, "create action", nil)
	}
	return nil
}

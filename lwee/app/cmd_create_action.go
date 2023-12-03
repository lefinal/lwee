package app

import (
	"context"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/projactionbuilder"
	"github.com/lefinal/meh"
)

// commandCreateProjectAction creates an action directory and template for a new
// action. The user is asked for input regarding the action name.
func commandCreateProjectAction(ctx context.Context, options commandOptions) error {
	logger := options.Logger
	// Create.
	containerEngine, err := container.NewEngine(logger.Named("container-engine"), options.LWEEConfig.ContainerEngineType, options.LWEEConfig.DisableCleanup)
	if err != nil {
		return meh.Wrap(err, "create container engine", nil)
	}
	err = containerEngine.Start(ctx)
	if err != nil {
		return meh.Wrap(err, "start container engine", nil)
	}
	defer containerEngine.Stop()
	builder := projactionbuilder.NewBuilder(logger, options.Locator, options.Input, containerEngine)
	err = builder.Build(ctx)
	if err != nil {
		return meh.Wrap(err, "create action", nil)
	}
	return nil
}

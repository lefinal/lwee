package app

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
)

// commandInit runs the CommandInit-command.
func commandInit(ctx context.Context, logger *zap.Logger, input input.Input, config Config) error {
	// Check if the directory is empty. Otherwise, request confirmation from the
	// user.
	contextDirEntries, err := os.ReadDir(config.ContextDir)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "read context directory", meh.Details{"context_dir": config.ContextDir})
	}
	if len(contextDirEntries) > 0 {
		logger.Debug(fmt.Sprintf("found %d entries in context directory", contextDirEntries))
		confirmed, err := input.RequestConfirm(ctx, "Directory not empty. Continue with potential data loss", false)
		if err != nil {
			return meh.Wrap(err, "request overwrite confirmation", nil)
		}
		if !confirmed {
			return meh.NewBadInputErr("canceled because of context directory not being empty",
				meh.Details{"context_dir": config.ContextDir})
		}
		logger.Debug("potential overwrite confirmed")
	}
	err = createInitFiles(logger)
	if err != nil {
		return meh.Wrap(err, "create init files", nil)
	}
	logger.Info("lwee project initialized")
	return nil
}

// createInitFiles creates the files for commandInit.
func createInitFiles(logger *zap.Logger) error {
	err := locator.Default().InitProject(logger)
	if err != nil {
		return meh.Wrap(err, "locator init", nil)
	}
	return nil
}

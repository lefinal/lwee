package app

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
)

// createInitFiles creates the files for commandInit.
func createInitFiles(logger *zap.Logger, locator *locator.Locator) error {
	err := locator.InitProject(logger)
	if err != nil {
		return meh.Wrap(err, "locator init", nil)
	}
	return nil
}

// commandInit initializes an empty LWEE project.
func commandInit(ctx context.Context, options commandOptions) error {
	logger := options.Logger
	// Check if the directory is empty. Otherwise, request confirmation from the
	// user.
	contextDirEntries, err := os.ReadDir(options.Locator.ContextDir())
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "read context directory", meh.Details{"context_dir": options.Locator.ContextDir()})
	}
	if len(contextDirEntries) > 0 {
		logger.Debug(fmt.Sprintf("found %d entries in context directory", contextDirEntries))
		confirmed, err := options.Input.RequestConfirm(ctx, "Directory not empty. Continue with potential data loss", false)
		if err != nil {
			return meh.Wrap(err, "request overwrite confirmation", nil)
		}
		if !confirmed {
			return meh.NewBadInputErr("canceled because of context directory not being empty",
				meh.Details{"context_dir": options.Locator.ContextDir()})
		}
		logger.Debug("potential overwrite confirmed")
	}
	// Init.
	err = createInitFiles(logger, options.Locator)
	if err != nil {
		return meh.Wrap(err, "create init files", nil)
	}
	logger.Info("lwee project initialized")
	return nil
}

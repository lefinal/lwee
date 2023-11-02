package app

import (
	"context"
	_ "embed"
	"fmt"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
	"path"
)

//go:embed init-flow.yaml
var defaultFlowFile []byte

// commandInit runs the CommandInit-command.
func commandInit(ctx context.Context, logger *zap.Logger, config Config) error {
	// Assure context directory is set and exists.
	if config.ContextDir == "" {
		return meh.NewBadInputErr("missing context directory", nil)
	}
	_, err := os.Stat(config.ContextDir)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "stat context directory", meh.Details{"context_dir": config.ContextDir})
	}
	// Check if the directory is empty. Otherwise, request confirmation from the
	// user.
	contextDirEntries, err := os.ReadDir(config.ContextDir)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "read context directory", meh.Details{"context_dir": config.ContextDir})
	}
	if len(contextDirEntries) > 0 {
		logger.Debug(fmt.Sprintf("found %d entries in context directory", contextDirEntries))
		confirmed, err := input.RequestConfirm(ctx, "Directory not empty. Continue with potential data loss?", false)
		if err != nil {
			return meh.Wrap(err, "request overwrite confirmation", nil)
		}
		if !confirmed {
			return meh.NewBadInputErr("aborted because of context directory not being empty",
				meh.Details{"context_dir": config.ContextDir})
		}
		logger.Debug("potential overwrite confirmed")
	}
	err = createInitFiles(logger, config.ContextDir)
	if err != nil {
		return meh.Wrap(err, "create init files", nil)
	}
	logger.Info("lwee project initialized")
	return nil
}

// createInitFiles creates the files for commandInit.
func createInitFiles(logger *zap.Logger, contextDir string) error {
	err := locator.Init(logger, contextDir)
	if err != nil {
		return meh.Wrap(err, "locator init", nil)
	}
	// Create flow file.
	flowFilename := path.Join(contextDir, DefaultFlowFilename)
	err = locator.CreateIfNotExists(flowFilename, defaultFlowFile)
	if err != nil {
		return meh.Wrap(err, "create default flow file", meh.Details{"filename": flowFilename})
	}
	return nil
}

package app

import (
	"context"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"path"
)

func commandVerify(_ context.Context, logger *zap.Logger, config Config) error {
	// Assure context directory is set.
	if config.ContextDir == "" {
		return meh.NewBadInputErr("missing context directory", nil)
	}
	// Parse.
	flowFilename := path.Join(config.ContextDir, config.FlowFilename)
	_, err := lweeflowfile.FromFile(flowFilename)
	if err != nil {
		return meh.Wrap(err, "flow from file", meh.Details{"flow_filename": flowFilename})
	}
	logger.Info("flow ok")
	return nil
}

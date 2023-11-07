package app

import (
	"context"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/lwee"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

func commandVerify(ctx context.Context, logger *zap.Logger, _ input.Input, config Config) error {
	// Parse flow.
	flowFilename := locator.Default().FlowFilename()
	flowFile, err := lweeflowfile.FromFile(flowFilename)
	if err != nil {
		return meh.Wrap(err, "flow from file", meh.Details{"flow_filename": flowFilename})
	}
	// Start a new LWEE runner with configuration.
	appLWEE, err := lwee.New(logger, flowFile, locator.Default(), lwee.Config{
		VerifyOnly:          true,
		ContainerEngineType: config.EngineType,
	})
	if err != nil {
		return meh.Wrap(err, "new lwee", nil)
	}
	err = appLWEE.Run(ctx)
	if err != nil {
		return meh.Wrap(err, "run lwee in verify mode", nil)
	}
	logger.Info("ok")
	return nil
}

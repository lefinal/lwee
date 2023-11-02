package app

import (
	"context"
	"github.com/lefinal/lwee/input"
	"github.com/lefinal/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

// commandCreateProjectAction creates an action directory and template for a new
// action. The user is asked for input regarding the action name.
func commandCreateProjectAction(ctx context.Context, logger *zap.Logger, config Config) error {
	// Request action name.
	actionName, err := input.RequestInput(ctx, "Action name (e.g., to-uppercase)", false)
	if err != nil {
		return meh.Wrap(err, "request action name", nil)
	}
	logger.Debug("got action name", zap.String("action_name", actionName))
	// Create.
	err = locator.Default().CreateProjectAction(actionName)
	if err != nil {
		return meh.Wrap(err, "create action", nil)
	}
	logger.Info("action created", zap.String("action_dir", locator.Default().ProjectActionDirByAction(actionName)))
	return nil
}

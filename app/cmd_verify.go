package app

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/locator"
	"github.com/lefinal/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lweeprojactionfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

func commandVerify(_ context.Context, logger *zap.Logger, config Config) error {
	// Parse flow.
	flowFilename := locator.Default().FlowFilename()
	flowFile, err := lweeflowfile.FromFile(flowFilename)
	if err != nil {
		return meh.Wrap(err, "flow from file", meh.Details{"flow_filename": flowFilename})
	}
	// Check referenced actions.
	for actionName, action := range flowFile.Actions {
		runner := action.Runner.Runner
		switch runner := runner.(type) {
		case lweeflowfile.ActionRunnerImage:
			actionFilename := locator.Default().ProjectActionLocatorByAction(actionName).ActionFilename()
			_, err := lweeprojactionfile.FromFile(actionFilename) // TODO: Verify action.
			if err != nil {
				return meh.Wrap(err, "read action file", meh.Details{
					"action_name":     actionName,
					"action_filename": actionFilename,
				})
			}
		default:
			return meh.NewBadInputErr(fmt.Sprintf("unsupported action runner type: %T", runner), nil)
		}
	}
	logger.Info("ok")
	return nil
}

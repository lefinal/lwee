package action

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/container"
	"github.com/lefinal/lwee/locator"
	"github.com/lefinal/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

type Action interface {
	Build(ctx context.Context) error
}

type Factory struct {
	FlowName        string
	Locator         *locator.Locator
	ContainerEngine container.Engine
}

func (factory *Factory) NewAction(logger *zap.Logger, action lweeflowfile.Action) (Action, error) {
	var builtAction Action
	var err error
	switch runner := action.Runner.Runner.(type) {
	case lweeflowfile.ActionRunnerProjectAction:
		builtAction, err = factory.newProjectAction(logger, runner)
		if err != nil {
			return nil, meh.Wrap(err, "new project action", nil)
		}
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported runner type: %T", runner), nil)
	}
	return builtAction, nil
}

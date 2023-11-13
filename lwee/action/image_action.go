package action

import (
	"context"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/lweestream"
	"github.com/lefinal/lwee/lwee/templaterender"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
	"sync"
	"time"
)

type imageActionExtraRenderData struct {
	WorkspaceHostDir  string
	WorkspaceMountDir string
}

func (factory *Factory) newImageAction(base *Base, renderData templaterender.Data, imageActionDetails lweeflowfile.ActionRunnerImage) (action, error) {
	// Render action runner details.
	workspaceHostDir := factory.Locator.ActionWorkspaceDirByAction(base.actionName)
	workspaceMountDir := factory.Locator.ContainerWorkspaceMountDir()
	renderData.Action.Extras = imageActionExtraRenderData{
		WorkspaceHostDir:  workspaceHostDir,
		WorkspaceMountDir: workspaceMountDir,
	}
	renderer := templaterender.New(renderData)
	err := imageActionDetails.Render(renderer)
	if err != nil {
		return nil, meh.Wrap(err, "render project action details", nil)
	}
	// Build the actual action.
	imageAction := &imageAction{
		imageRunner: imageRunner{
			Base:                 base,
			containerEngine:      factory.ContainerEngine,
			imageTag:             imageActionDetails.Image,
			command:              imageActionDetails.Command,
			env:                  imageActionDetails.Env,
			workspaceHostDir:     workspaceHostDir,
			workspaceMountDir:    workspaceMountDir,
			containerState:       containerStateReady,
			containerRunningCond: sync.NewCond(&sync.Mutex{}),
			streamConnector:      lweestream.NewConnector(base.logger.Named("stream-connector")),
		},
	}
	err = os.MkdirAll(imageAction.workspaceHostDir, 0750)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create workspace dir",
			meh.Details{"dir": imageAction.workspaceHostDir})
	}
	return imageAction, nil
}

// imageAction is an Action that runs an external container image.
type imageAction struct {
	imageRunner
}

func (action *imageAction) Build(ctx context.Context) error {
	start := time.Now()
	action.logger.Debug("pull image", zap.String("image_tag", action.imageTag))
	err := action.containerEngine.ImagePull(ctx, action.imageTag)
	if err != nil {
		return meh.Wrap(err, "pull image", meh.Details{"image_tag": action.imageTag})
	}
	action.logger.Debug("image pull completed", zap.Duration("took", time.Since(start)))
	return nil
}

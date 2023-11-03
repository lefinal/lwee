package action

import (
	"context"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/lweestream"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
	"path"
	"sync"
	"time"
)

func (factory *Factory) newImageAction(base *Base, imageActionDetails lweeflowfile.ActionRunnerImage) (action, error) {
	imageAction := &imageAction{
		imageRunner: imageRunner{
			Base:                  base,
			containerEngine:       factory.ContainerEngine,
			imageTag:              imageActionDetails.Image,
			command:               imageActionDetails.Command,
			containerWorkspaceDir: path.Join(factory.Locator.ActionTempDirByAction(base.actionName), "container-workspace"),
			containerState:        containerStateReady,
			containerRunningCond:  sync.NewCond(&sync.Mutex{}),
			streamConnector:       lweestream.NewConnector(base.logger.Named("stream-connector")),
		},
	}
	err := os.MkdirAll(imageAction.containerWorkspaceDir, 0750)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "create container workspace dir",
			meh.Details{"dir": imageAction.containerWorkspaceDir})
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

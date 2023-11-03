package action

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/lweeprojactionfile"
	"github.com/lefinal/lwee/lwee/lweestream"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
	"path"
	"sync"
	"time"
)

func (factory *Factory) newProjectAction(base *Base, projectActionDetails lweeflowfile.ActionRunnerProjectAction) (action, error) {
	// Assure action exists.
	actionDir := factory.Locator.ProjectActionDirByAction(projectActionDetails.Name)
	_, err := os.Stat(actionDir)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "stat action dir", meh.Details{"action_dir": actionDir})
	}
	actionLocator := factory.Locator.ProjectActionLocatorByAction(projectActionDetails.Name)
	// Read action.
	projectActionFile, err := lweeprojactionfile.FromFile(actionLocator.ActionFilename())
	if err != nil {
		return nil, meh.Wrap(err, "project action file from file",
			meh.Details{"filename": actionLocator.ActionFilename()})
	}
	if projectActionDetails.Config == "" {
		projectActionDetails.Config = "default"
	}
	actionConfig, ok := projectActionFile.Configs[projectActionDetails.Config]
	if !ok {
		knownConfigs := make([]string, 0)
		for configName := range projectActionFile.Configs {
			knownConfigs = append(knownConfigs, configName)
		}
		return nil, meh.NewBadInputErr(fmt.Sprintf("unknown action config: %s", projectActionDetails.Config),
			meh.Details{"known_configs": knownConfigs})
	}
	// Create actual action.
	switch actionConfig := actionConfig.(type) {
	case lweeprojactionfile.ProjActionConfigImage:
		if actionConfig.File == "" {
			actionConfig.File = "Dockerfile"
		}
		projectActionImage := &projectActionImage{
			imageRunner: imageRunner{
				Base:                  base,
				containerEngine:       factory.ContainerEngine,
				imageTag:              projectActionImageTag(factory.FlowName, projectActionDetails.Name),
				command:               projectActionDetails.Command,
				containerWorkspaceDir: path.Join(factory.Locator.ActionTempDirByAction(base.actionName), "container-workspace"),
				containerState:        containerStateReady,
				containerRunningCond:  sync.NewCond(&sync.Mutex{}),
				streamConnector:       lweestream.NewConnector(base.logger.Named("stream-connector")),
			},
			contextDir: actionDir,
			file:       actionConfig.File,
		}
		err := os.MkdirAll(projectActionImage.containerWorkspaceDir, 0750)
		if err != nil {
			return nil, meh.NewInternalErrFromErr(err, "create container workspace dir",
				meh.Details{"dir": projectActionImage.containerWorkspaceDir})
		}
		return projectActionImage, nil
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported action config: %T", actionConfig), nil)
	}
}

// projectActionImage is an Action that runs a project action with type image.
type projectActionImage struct {
	imageRunner
	contextDir string
	file       string
}

func (action *projectActionImage) Build(ctx context.Context) error {
	imageBuildOptions := container.ImageBuildOptions{
		BuildLogger: action.logger.Named("build"),
		Tag:         action.imageTag,
		ContextDir:  action.contextDir,
		File:        action.file,
	}
	start := time.Now()
	action.logger.Debug("build project action image",
		zap.String("image_tag", imageBuildOptions.Tag),
		zap.String("context_dir", imageBuildOptions.ContextDir),
		zap.String("file", imageBuildOptions.File))
	err := action.containerEngine.ImageBuild(ctx, imageBuildOptions)
	if err != nil {
		return meh.Wrap(err, "build image", meh.Details{"image_build_options": imageBuildOptions})
	}
	action.logger.Debug("project action image build done", zap.Duration("took", time.Since(start)))
	return nil
}

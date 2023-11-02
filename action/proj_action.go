package action

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/container"
	"github.com/lefinal/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lweeprojactionfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os"
	"strings"
	"time"
	"unicode"
)

func (factory *Factory) newProjectAction(logger *zap.Logger, actionFile lweeflowfile.ActionRunnerProjectAction) (Action, error) {
	// Assure action exists.
	actionDir := factory.Locator.ProjectActionDirByAction(actionFile.Name)
	_, err := os.Stat(actionDir)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "stat action dir", meh.Details{"action_dir": actionDir})
	}
	actionLocator := factory.Locator.ProjectActionLocatorByAction(actionFile.Name)
	// Read action.
	projectActionFile, err := lweeprojactionfile.FromFile(actionLocator.ActionFilename())
	if err != nil {
		return nil, meh.Wrap(err, "project action file from file",
			meh.Details{"filename": actionLocator.ActionFilename()})
	}
	if actionFile.Config == "" {
		actionFile.Config = "default"
	}
	actionConfig, ok := projectActionFile.Configs[actionFile.Config]
	if !ok {
		knownConfigs := make([]string, 0)
		for configName := range projectActionFile.Configs {
			knownConfigs = append(knownConfigs, configName)
		}
		return nil, meh.NewBadInputErr(fmt.Sprintf("unknown action config: %s", actionFile.Config),
			meh.Details{"known_configs": knownConfigs})
	}
	switch actionConfig := actionConfig.(type) {
	case lweeprojactionfile.ProjActionConfigImage:
		if actionConfig.File == "" {
			actionConfig.File = "Dockerfile"
		}
		return &ProjectActionImage{
			Logger:          logger,
			ContainerEngine: factory.ContainerEngine,
			ContextDir:      actionDir,
			File:            actionConfig.File,
			Tag:             projectActionImageTag(factory.FlowName, actionFile.Name),
			Args:            actionFile.Args,
		}, nil
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported action config: %T", actionConfig), nil)
	}
}

// ProjectActionImage is an Action that runs a project action with type image.
type ProjectActionImage struct {
	Logger          *zap.Logger
	ContainerEngine container.Engine
	ContextDir      string
	File            string
	Tag             string
	Args            string
}

func (action *ProjectActionImage) Build(ctx context.Context) error {
	imageBuildOptions := container.ImageBuildOptions{
		BuildLogger: action.Logger.Named("build"),
		Tag:         action.Tag,
		ContextDir:  action.ContextDir,
		File:        action.File,
	}
	start := time.Now()
	action.Logger.Debug("build project action image",
		zap.String("image_tag", imageBuildOptions.Tag),
		zap.String("context_dir", imageBuildOptions.ContextDir),
		zap.String("file", imageBuildOptions.File))
	err := action.ContainerEngine.ImageBuild(ctx, imageBuildOptions)
	if err != nil {
		return meh.Wrap(err, "build image", meh.Details{"image_build_options": imageBuildOptions})
	}
	action.Logger.Debug("project action image build done", zap.Duration("took", time.Since(start)))
	return nil
}

func projectActionImageTag(flowName string, actionName string) string {
	const replaceNonAlphanumericWith = '_'
	flowName = toAlphanumeric(flowName, replaceNonAlphanumericWith)
	flowName = strings.ToLower(flowName)
	actionName = toAlphanumeric(actionName, replaceNonAlphanumericWith)
	actionName = strings.ToLower(actionName)
	return fmt.Sprintf("lwee__%s__%s", flowName, actionName)
}

func toAlphanumeric(str string, replaceOthersWith rune) string {
	var out strings.Builder
	for _, r := range str {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			r = replaceOthersWith
		}
		out.WriteRune(r)
	}
	return out.String()
}

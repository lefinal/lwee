package projactionbuilder

import (
	"context"
	"embed"
	"fmt"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strings"
)

const goImage = "golang:1.21"

type Builder struct {
	logger          *zap.Logger
	locator         *locator.Locator
	input           input.Input
	containerEngine container.Engine
	actionName      string
}

func NewBuilder(logger *zap.Logger, locator *locator.Locator, input input.Input, containerEngine container.Engine) *Builder {
	return &Builder{
		logger:          logger,
		locator:         locator,
		input:           input,
		containerEngine: containerEngine,
	}
}

func (builder *Builder) Build(ctx context.Context) error {
	var err error
	// Request the action name.
	builder.actionName, err = builder.input.Request(ctx, "Action name (e.g., to-uppercase)", false)
	if err != nil {
		return meh.Wrap(err, "request action name", nil)
	}
	builder.logger.Debug("got action name", zap.String("action_name", builder.actionName))
	// Request the template name.
	templateName, err := builder.input.Request(ctx, "Template (go, go-no-sdk, none) [none]", true)
	if err != nil {
		return meh.Wrap(err, "request template", nil)
	}
	var buildTemplate func(ctx context.Context) error
	switch templateName {
	case "go":
		buildTemplate = builder.buildGoTemplate
	case "go-no-sdk":
		buildTemplate = builder.buildGoTemplateNoSDK
	case "none", "":
	default:
		return meh.NewBadInputErr(fmt.Sprintf("unsupported template: %s", templateName), nil)
	}
	// Build action and template.
	err = builder.buildAction()
	if err != nil {
		return meh.Wrap(err, "build action", nil)
	}
	if buildTemplate != nil {
		err = buildTemplate(ctx)
		if err != nil {
			return meh.Wrap(err, "build template", meh.Details{"template_name": templateName})
		}
	}
	builder.logger.Info("action created", zap.String("action_dir", builder.locator.ProjectActionDirByAction(builder.actionName)))
	return nil
}

func (builder *Builder) buildAction() error {
	actionDir := builder.locator.ProjectActionDirByAction(builder.actionName)
	// Assure action dir does not yet exist.
	_, err := os.Stat(actionDir)
	if err == nil {
		return meh.NewBadInputErr("action already exists", meh.Details{"action_dir": actionDir})
	}
	// Create action dir.
	err = locator.CreateDirIfNotExists(actionDir)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create action dir", meh.Details{"action_dir": actionDir})
	}
	// Create action file.
	actionLocator := builder.locator.ProjectActionLocatorByAction(builder.actionName)
	actionFilename := actionLocator.ActionFilename()
	err = locator.CreateIfNotExists(actionFilename, actionYAML)
	if err != nil {
		return meh.Wrap(err, "create action file", meh.Details{"action_filename": actionFilename})
	}
	return nil
}

func (builder *Builder) buildGoTemplate(ctx context.Context) error {
	actionLocator := builder.locator.ProjectActionLocatorByAction(builder.actionName)
	currentUser, err := user.Current()
	if err != nil {
		return meh.NewInternalErrFromErr(err, "get current user", nil)
	}
	// Prepare volume mounts that are mounted for all commands that are run in containers.
	actionVolumeMounts := []container.VolumeMount{
		{
			Source: actionLocator.ActionDir(),
			Target: "/action",
		},
	}
	// Check whether we have a local Go build-cache available.
	goBuildCacheDir := path.Join(currentUser.HomeDir, ".cache", "go-build")
	if _, err = os.Stat(goBuildCacheDir); err == nil {
		builder.logger.Debug("found local go build-cache", zap.String("cache_dir", goBuildCacheDir))
		actionVolumeMounts = append(actionVolumeMounts, container.VolumeMount{
			Source: goBuildCacheDir,
			Target: "/tmp/go-build-cache",
		})
	}
	// Check whether we have a local Go path available.
	goPathRaw, err := exec.CommandContext(ctx, "go", "env", "GOPATH").Output()
	if err == nil {
		goPath := strings.TrimSpace(string(goPathRaw))
		builder.logger.Debug("found local go path", zap.String("go_path", goPath))
		actionVolumeMounts = append(actionVolumeMounts, container.VolumeMount{
			Source: goPath,
			Target: "/tmp/go",
		})
	}
	// Initialize the module.
	builder.logger.Debug("initialize go module")
	err = builder.containerEngine.RunContainer(ctx, container.Config{
		VolumeMounts: actionVolumeMounts,
		WorkingDir:   "/action",
		Command:      []string{"go", "mod", "init", builder.actionName},
		Image:        goImage,
		User:         currentUser.Uid,
	})
	if err != nil {
		return meh.Wrap(err, "run module init container", nil)
	}
	// Copy the template files.
	builder.logger.Debug("copy template files")
	err = copyEmbedDir(templateResources, "resources/go-template", actionLocator.ActionDir(), ".", nil)
	if err != nil {
		return meh.Wrap(err, "copy embed dir", nil)
	}
	// Tidy up.
	builder.logger.Debug("tidy action")
	err = builder.containerEngine.RunContainer(ctx, container.Config{
		VolumeMounts: actionVolumeMounts,
		WorkingDir:   "/action",
		Command:      []string{"/bin/bash", "/action/tidy.sh"},
		Image:        goImage,
		User:         currentUser.Uid,
	})
	if err != nil {
		return meh.Wrap(err, "run tidy container", nil)
	}
	tidyScriptFilename := path.Join(actionLocator.ActionDir(), "tidy.sh")
	err = os.Remove(tidyScriptFilename)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "remove tidy script", meh.Details{"filename": tidyScriptFilename})
	}
	return nil
}

func (builder *Builder) buildGoTemplateNoSDK(ctx context.Context) error {
	actionLocator := builder.locator.ProjectActionLocatorByAction(builder.actionName)
	currentUser, err := user.Current()
	if err != nil {
		return meh.NewInternalErrFromErr(err, "get current user", nil)
	}
	// Prepare volume mounts that are mounted for all commands that are run in containers.
	actionVolumeMounts := []container.VolumeMount{
		{
			Source: actionLocator.ActionDir(),
			Target: "/action",
		},
	}
	// Initialize the module.
	builder.logger.Debug("initialize go module")
	err = builder.containerEngine.RunContainer(ctx, container.Config{
		VolumeMounts: actionVolumeMounts,
		WorkingDir:   "/action",
		Command:      []string{"go", "mod", "init", builder.actionName},
		Image:        goImage,
		User:         currentUser.Uid,
	})
	if err != nil {
		return meh.Wrap(err, "run module init container", nil)
	}
	// Copy the template files.
	builder.logger.Debug("copy template files")
	err = copyEmbedDir(templateResources, "resources/go-template-no-sdk", actionLocator.ActionDir(), ".", nil)
	if err != nil {
		return meh.Wrap(err, "copy embed dir", nil)
	}
	return nil
}

func copyEmbedDir(src embed.FS, srcSubDir string, dstDir string, currentDir string, blacklist []string) error {
	currentSubDir := path.Join(srcSubDir, currentDir)
	dirEntries, err := src.ReadDir(currentSubDir)
	if err != nil {
		return meh.NewInternalErrFromErr(err, fmt.Sprintf("read source directory %q", currentSubDir), nil)
	}
	for _, dirEntry := range dirEntries {
		// If directory, call recursively.
		if dirEntry.IsDir() {
			dirName := path.Join(currentDir, dirEntry.Name())
			err = locator.CreateDirIfNotExists(dirName)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("create dir %s", dirName), nil)
			}
			err = copyEmbedDir(src, srcSubDir, dstDir, dirName, blacklist)
			if err != nil {
				return err
			}
		} else {
			// If it is a file, copy it after checking the blacklist.
			srcFilename := path.Join(currentSubDir, dirEntry.Name())
			dstFilename := path.Join(dstDir, currentDir, dirEntry.Name())
			dstFilename = strings.TrimSuffix(dstFilename, ".lweetemplate") // Remove template suffix.
			blacklisted, err := isBlacklisted(srcFilename, blacklist)
			if err != nil {
				return meh.Wrap(err, "check if blacklisted", meh.Details{
					"filename":  srcFilename,
					"blacklist": blacklist,
				})
			}
			if blacklisted {
				continue
			}
			srcFile, err := src.Open(srcFilename)
			if err != nil {
				return meh.NewInternalErrFromErr(err, fmt.Sprintf("open embedded file %s", srcFilename), nil)
			}
			defer func() { _ = srcFile.Close() }()
			err = locator.CreateIfNotExists(dstFilename, []byte{})
			if err != nil {
				return meh.Wrap(err, "create file", nil)
			}
			dstFile, err := os.Create(dstFilename)
			if err != nil {
				return meh.NewInternalErrFromErr(err, fmt.Sprintf("open created file %s", dstFilename), nil)
			}
			defer func() { _ = dstFile.Close() }()
			_, err = io.Copy(dstFile, srcFile)
			if err != nil {
				return meh.Wrap(err, "copy file", meh.Details{
					"src_filename": srcFilename,
					"dst_filename": dstFilename,
				})
			}
		}
	}
	return nil
}

func isBlacklisted(name string, blacklist []string) (bool, error) {
	for _, blacklistEntry := range blacklist {
		blacklistEntryRegex, err := regexp.Compile(blacklistEntry)
		if err != nil {
			return false, meh.NewInternalErrFromErr(err, "compile blacklist entry", meh.Details{"entry": blacklistEntry})
		}
		if blacklistEntryRegex.MatchString(name) {
			return true, nil
		}
	}
	return false, nil
}

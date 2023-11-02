package locator

import (
	"github.com/lefinal/lwee/defaults"
	"github.com/lefinal/meh"
	"os"
	"path"
)

const (
	actionsDir     = "./actions"
	actionFilename = "action.yaml"
)

func (locator *Locator) ProjectActionDirByAction(actionName string) string {
	actionsDir := path.Join(locator.contextDir, actionsDir)
	return path.Join(actionsDir, actionName)
}

func (locator *Locator) CreateProjectAction(actionName string) error {
	actionDir := locator.ProjectActionDirByAction(actionName)
	// Assure action dir does not yet exist.
	_, err := os.Stat(actionDir)
	if err == nil {
		return meh.NewBadInputErr("action already exists", meh.Details{"action_dir": actionDir})
	}
	// Create action dir.
	err = os.MkdirAll(actionDir, mkdirPerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create action dir", meh.Details{"action_dir": actionDir})
	}
	// Create action file.
	actionFilename := path.Join(actionDir, actionFilename)
	err = CreateIfNotExists(actionFilename, defaults.Action)
	if err != nil {
		return meh.Wrap(err, "create action file", meh.Details{"action_filename": actionFilename})
	}
	return nil
}

func (locator *Locator) ProjectActionLocatorByAction(actionName string) *ProjectActionLocator {
	return &ProjectActionLocator{actionDir: locator.ProjectActionDirByAction(actionName)}
}

type ProjectActionLocator struct {
	actionDir string
}

func (locator *ProjectActionLocator) ActionFilename() string {
	return path.Join(locator.actionDir, actionFilename)
}

package locator

import (
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

func (locator *Locator) ProjectActionLocatorByAction(actionName string) *ProjectActionLocator {
	return &ProjectActionLocator{actionDir: locator.ProjectActionDirByAction(actionName)}
}

type ProjectActionLocator struct {
	actionDir string
}

func (locator *ProjectActionLocator) ActionDir() string {
	return locator.actionDir
}

func (locator *ProjectActionLocator) ActionFilename() string {
	return path.Join(locator.actionDir, actionFilename)
}

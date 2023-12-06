package locator

import (
	"path/filepath"
)

const (
	actionsDir     = "./actions"
	actionFilename = "action.yaml"
)

// ProjectActionDirByAction returns the directory path of the specified action in
// the project.
func (locator *Locator) ProjectActionDirByAction(actionName string) string {
	actionsDir := filepath.Join(locator.contextDir, actionsDir)
	return filepath.Join(actionsDir, actionName)
}

// ProjectActionLocatorByAction returns a new ProjectActionLocator initialized
// with the directory path of the specified action.
func (locator *Locator) ProjectActionLocatorByAction(actionName string) *ProjectActionLocator {
	return &ProjectActionLocator{actionDir: locator.ProjectActionDirByAction(actionName)}
}

// ProjectActionLocator is a locator for project actions. Create one using
// Locator.ProjectActionLocatorByAction.
type ProjectActionLocator struct {
	actionDir string
}

// ActionDir returns the directory path of the action.
func (locator *ProjectActionLocator) ActionDir() string {
	return locator.actionDir
}

// ActionFilename returns the path of the action file for the action.
func (locator *ProjectActionLocator) ActionFilename() string {
	return filepath.Join(locator.actionDir, actionFilename)
}

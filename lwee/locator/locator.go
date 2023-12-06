// Package locator centralizes locating directories and files into a single
// Locator that can use a configured context directory.
package locator

import (
	"bytes"
	"github.com/lefinal/lwee/lwee/defaults"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	lweeDir    = "./.lwee"
	sourcesDir = "./src"
)

const mkdirPerm = 0750
const filePerm = 0644

// FindContextDir tries to locate an LWEE context directory in the given
// directory or any of its parents. If an LWEE directory has been found, its
// context path is returned. Otherwise, an error is returned.
func FindContextDir(startDir string) (string, error) {
	useAsContextDir := func(dir string) error {
		locator, err := New(dir, "should-not-exist.yaml")
		if err != nil {
			return meh.Wrap(err, "new locator", nil)
		}
		err = locator.AssureLWEEProject()
		if err != nil {
			return meh.Wrap(err, "assure lwee project", nil)
		}
		return nil
	}

	// Get absolute context dir.
	startDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", meh.Wrap(err, "get absolute path for start dir", meh.Details{"start_dir": startDir})
	}
	// Find in start directory or parents.
	currentDir := startDir
	var firstErr error
	for {
		err := useAsContextDir(currentDir)
		if err == nil {
			// Ok.
			return currentDir, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		// Try parent directory.
		if currentDir == "/" {
			return "", meh.NewBadInputErr("cannot find lwee project in this or any parent directory", meh.Details{
				"find_err":    firstErr.Error(),
				"start_dir":   startDir,
				"current_dir": currentDir,
			})
		}
		currentDir = filepath.Dir(currentDir)
	}
}

// Locator provides a centralized way of locating files within a project.
type Locator struct {
	contextDir    string
	flowFilename  string
	actionTempDir string
}

// New creates a new Locator.
func New(contextDir string, flowFilename string) (*Locator, error) {
	_, err := os.Stat(contextDir)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "stat context directory", meh.Details{"context_dir": contextDir})
	}
	// Get absolute context dir.
	contextDir, err = filepath.Abs(contextDir)
	if err != nil {
		return nil, meh.Wrap(err, "get absolute path for context dir", meh.Details{"context_dir": contextDir})
	}
	actionTempDir := filepath.Join(os.TempDir(), "lwee",
		time.Now().Format("2006-01-02_15-04-05")+"_"+strconv.Itoa(rand.Intn(999999)))
	return &Locator{
		contextDir:    contextDir,
		flowFilename:  flowFilename,
		actionTempDir: actionTempDir,
	}, nil
}

// AssureLWEEProject returns an error if the project is not an LWEE project. The
// check is done by checking the LWEEDir for existence.
func (locator *Locator) AssureLWEEProject() error {
	_, err := os.Stat(locator.LWEEDir())
	if err != nil {
		return err
	}
	return nil
}

// FlowFilename returns the flow filename associated with the Locator.
func (locator *Locator) FlowFilename() string {
	return locator.flowFilename
}

// ContextDir returns the context directory path.
func (locator *Locator) ContextDir() string {
	return locator.contextDir
}

// InitProject creates the necessary directories and files for a new project.
//
// It first creates the actions directory and ensures it is empty. Then it
// creates the sources directory and ensures it is empty. After that, it creates
// the flow file if it does not already exist. Finally, it creates the LWEE
// directory and ensures it contains a .gitkeep file.
func (locator *Locator) InitProject(logger *zap.Logger) error {
	// Create actions directory.
	actionsDir := filepath.Join(locator.contextDir, actionsDir)
	logger.Debug("create actions directory", zap.String("dir", actionsDir))
	err := os.MkdirAll(actionsDir, mkdirPerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create actions directory", meh.Details{"dir": actionsDir})
	}
	err = gitKeepDir(actionsDir)
	if err != nil {
		return meh.Wrap(err, "git keep actions directory", meh.Details{"dir": actionsDir})
	}
	// Create sources directory.
	sourcesDir := filepath.Join(locator.contextDir, sourcesDir)
	logger.Debug("create sources directory", zap.String("dir", sourcesDir))
	err = os.MkdirAll(sourcesDir, mkdirPerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create sources directory", meh.Details{"dir": sourcesDir})
	}
	err = gitKeepDir(sourcesDir)
	if err != nil {
		return meh.Wrap(err, "git keep sources directory", meh.Details{"dir": sourcesDir})
	}
	// Create flow file.
	err = CreateIfNotExists(locator.flowFilename, defaults.FlowFile)
	if err != nil {
		return meh.Wrap(err, "create default flow file", meh.Details{"filename": locator.flowFilename})
	}

	// After all, create the LWEE directory.
	err = os.MkdirAll(locator.LWEEDir(), mkdirPerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create lwee directory", meh.Details{"dir": locator.LWEEDir()})
	}
	err = os.WriteFile(filepath.Join(locator.LWEEDir(), ".gitkeep"), nil, filePerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create .gitkeep in lwee directory", meh.Details{"dir": locator.LWEEDir()})
	}
	return nil
}

// CreateDirIfNotExists checks if a directory exists. If it does not, it creates
// the directory. If the file descriptor already exists and is a file, it returns
// an error.
func CreateDirIfNotExists(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		// Already exists.
		if !info.IsDir() {
			return meh.NewInternalErr("directory is a file", nil)
		}
		return nil
	}
	// Create.
	err = os.MkdirAll(dir, mkdirPerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "mkdir all", nil)
	}
	return nil
}

// CreateIfNotExists checks if a file exists and creates it if not. If the file
// already exists, it does not perform any action and returns nil.
func CreateIfNotExists(filename string, content []byte) error {
	err := CreateDirIfNotExists(filepath.Dir(filename))
	if err != nil {
		return meh.Wrap(err, "create dir if not exists", meh.Details{"dir": filepath.Dir(filename)})
	}
	_, err = os.Stat(filename)
	if err == nil {
		// Already exists.
		return nil
	}
	// Create.
	f, err := os.Create(filename)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create file", nil)
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, bytes.NewReader(content))
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "write file", nil)
	}
	err = f.Close()
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "close written file", nil)
	}
	return nil
}

// LWEEDir returns the path to the LWEE directory within the project.
func (locator *Locator) LWEEDir() string {
	return filepath.Join(locator.contextDir, lweeDir)
}

// ActionTempDirByAction returns the temporary directory path for a specific
// action. Make sure to create it yourself.
func (locator *Locator) ActionTempDirByAction(actionName string) string {
	return filepath.Join(locator.actionTempDir, ToAlphanumeric(actionName, '_'))
}

// ActionTempDir returns the action temporary directory.
func (locator *Locator) ActionTempDir() string {
	return locator.actionTempDir
}

// ActionWorkspaceDirByAction returns the workspace directory for a specific
// action. Make sure to create it yourself.
func (locator *Locator) ActionWorkspaceDirByAction(actionName string) string {
	return filepath.Join(locator.ActionTempDirByAction(actionName), "workspace")
}

// ContainerWorkspaceMountDir returns the path of the directory to mount as the
// workspace directory in the container.
func (locator *Locator) ContainerWorkspaceMountDir() string {
	return "/lwee"
}

// RunInfoYAMLFilename returns the path to the run info file within the project.
func (locator *Locator) RunInfoYAMLFilename() string {
	return filepath.Join(locator.contextDir, "out", "run-info.yaml")
}

// gitKeepDir creates a .gitkeep file in the specified directory if it does not
// exist.
func gitKeepDir(dir string) error {
	gitKeepFilename := filepath.Join(dir, ".gitkeep")
	err := CreateIfNotExists(gitKeepFilename, []byte{})
	if err != nil {
		return meh.Wrap(err, "create .gitkeep", meh.Details{"filename": gitKeepFilename})
	}
	return nil
}

// ToAlphanumeric replaces all non-alphanumeric characters in a string with the
// provided replacement rune. It returns the modified string.
func ToAlphanumeric(str string, replaceOthersWith rune) string {
	var out strings.Builder
	for _, r := range str {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			r = replaceOthersWith
		}
		out.WriteRune(r)
	}
	return out.String()
}

package locator

import (
	"bytes"
	"github.com/lefinal/lwee/lwee/defaults"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"math/rand"
	"os"
	"path"
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
		currentDir = path.Dir(currentDir)
	}
}

type Locator struct {
	contextDir    string
	flowFilename  string
	actionTempDir string
}

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
	actionTempDir := path.Join(os.TempDir(), "lwee",
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

func (locator *Locator) FlowFilename() string {
	return locator.flowFilename
}

func (locator *Locator) ContextDir() string {
	return locator.contextDir
}

func (locator *Locator) InitProject(logger *zap.Logger) error {
	// Create actions directory.
	actionsDir := path.Join(locator.contextDir, actionsDir)
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
	sourcesDir := path.Join(locator.contextDir, sourcesDir)
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
	err = os.WriteFile(path.Join(locator.LWEEDir(), ".gitkeep"), nil, filePerm)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create .gitkeep in lwee directory", meh.Details{"dir": locator.LWEEDir()})
	}
	return nil
}

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

func CreateIfNotExists(filename string, content []byte) error {
	err := CreateDirIfNotExists(path.Dir(filename))
	if err != nil {
		return meh.Wrap(err, "create dir if not exists", meh.Details{"dir": path.Dir(filename)})
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

func (locator *Locator) LWEEDir() string {
	return path.Join(locator.contextDir, lweeDir)
}

func (locator *Locator) ActionTempDirByAction(actionName string) string {
	return path.Join(locator.actionTempDir, ToAlphanumeric(actionName, '_'))
}

func (locator *Locator) ActionTempDir() string {
	return locator.actionTempDir
}

func (locator *Locator) ActionWorkspaceDirByAction(actionName string) string {
	return path.Join(locator.ActionTempDirByAction(actionName), "workspace")
}

func (locator *Locator) ContainerWorkspaceMountDir() string {
	return "/lwee"
}

func (locator *Locator) RunInfoYAMLFilename() string {
	return path.Join(locator.contextDir, "out", "run-info.yaml")
}

func gitKeepDir(dir string) error {
	gitKeepFilename := path.Join(dir, ".gitkeep")
	err := CreateIfNotExists(gitKeepFilename, []byte{})
	if err != nil {
		return meh.Wrap(err, "create .gitkeep", meh.Details{"filename": gitKeepFilename})
	}
	return nil
}

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

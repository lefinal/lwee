package locator

import (
	"bytes"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"os"
	"path"
)

const (
	actionsDir = "./actions"
	sourcesDir = "./src"
)

func CreateIfNotExists(filename string, content []byte) error {
	_, err := os.Stat(filename)
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

func gitKeepDir(dir string) error {
	gitKeepFilename := path.Join(dir, ".gitkeep")
	err := CreateIfNotExists(gitKeepFilename, []byte{})
	if err != nil {
		return meh.Wrap(err, "create .gitkeep", meh.Details{"filename": gitKeepFilename})
	}
	return nil
}

func Init(logger *zap.Logger, contextDir string) error {
	// Create actions directory.
	actionsDir := path.Join(contextDir, actionsDir)
	logger.Debug("create actions directory", zap.String("dir", actionsDir))
	err := os.MkdirAll(actionsDir, 0750)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create actions directory", meh.Details{"dir": actionsDir})
	}
	err = gitKeepDir(actionsDir)
	if err != nil {
		return meh.Wrap(err, "git keep actions directory", meh.Details{"dir": actionsDir})
	}
	// Create sources directory.
	sourcesDir := path.Join(contextDir, sourcesDir)
	logger.Debug("create sources directory", zap.String("dir", sourcesDir))
	err = os.MkdirAll(sourcesDir, 0750)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "create sources directory", meh.Details{"dir": sourcesDir})
	}
	err = gitKeepDir(sourcesDir)
	if err != nil {
		return meh.Wrap(err, "git keep sources directory", meh.Details{"dir": sourcesDir})
	}
	return nil
}

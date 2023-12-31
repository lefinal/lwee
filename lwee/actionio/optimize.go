package actionio

import (
	"github.com/lefinal/meh"
	"os"
	"path/filepath"
)

// ApplyFileCopyOptimization optimizes file copying by moving the source file to
// the destination. If the move fails, it returns an error and copies the file
// regularly.
func ApplyFileCopyOptimization(src string, dst string) error {
	err := os.MkdirAll(filepath.Dir(dst), 0750)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "mkdir all", meh.Details{"dir": filepath.Dir(dst)})
	}
	err = os.Rename(src, dst)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "copy file", nil)
	}
	return nil
}

package app

import (
	"context"
	"github.com/lefinal/meh"
)

// commandRun runs a workflow in the context directory.
func commandRun(ctx context.Context, options commandOptions) error {
	// Start a new LWEE runner with configuration.
	appLWEE, err := newLWEEWithOptions(options)
	if err != nil {
		return meh.Wrap(err, "new lwee", nil)
	}
	err = appLWEE.Run(ctx)
	if err != nil {
		return meh.Wrap(err, "run lwee", nil)
	}
	return nil
}

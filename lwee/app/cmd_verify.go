package app

import (
	"context"
	"github.com/lefinal/meh"
)

func commandVerify(ctx context.Context, options commandOptions) error {
	options.LWEEConfig.VerifyOnly = true
	appLWEE, err := newLWEEWithOptions(options)
	if err != nil {
		return meh.Wrap(err, "new lwee", nil)
	}
	err = appLWEE.Run(ctx)
	if err != nil {
		return meh.Wrap(err, "run lwee in verify mode", nil)
	}
	options.Logger.Info("ok")
	return nil
}

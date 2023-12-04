// Package main is the base package for running the application.
package main

import (
	"context"
	_ "embed"
	"github.com/lefinal/lwee/lwee/app"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/waitforterminate"
	"github.com/lefinal/meh/mehlog"
	"os"
)

func main() {
	err := waitforterminate.Run(run)
	if err != nil {
		mehlog.Log(logging.RootLogger(), err)
		_ = logging.RootLogger().Sync()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	return app.RunCLI(ctx, nil, os.Args)
}

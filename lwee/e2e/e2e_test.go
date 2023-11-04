package e2e

import (
	"context"
	"github.com/lefinal/lwee/lwee/app"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/meh/mehlog"
	"github.com/lefinal/zaprec"
	"go.uber.org/zap"
	"testing"
)

type config struct {
	command      string
	contextDir   string
	flowFilename string
}

func run(t *testing.T, config config) error {
	logger, records := zaprec.NewRecorder(zap.DebugLevel)
	t.Cleanup(func() {
		if t.Failed() {
			logger, _ = logging.NewLogger(zap.DebugLevel)
			records.DumpToLogger(logger)
		}
	})

	err := app.Run(context.Background(), logger, app.Config{
		EngineType:         container.EngineTypeDocker,
		Command:            config.command,
		ContextDir:         config.contextDir,
		FlowFilename:       config.flowFilename,
		KeepTemporaryFiles: false,
	})
	if err != nil {
		mehlog.Log(logger, err)
	}
	return err
}

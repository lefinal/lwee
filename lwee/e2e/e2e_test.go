package e2e

import (
	"context"
	"github.com/lefinal/lwee/lwee/app"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/meh/mehlog"
	"github.com/lefinal/zaprec"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"os"
	"testing"
)

type config struct {
	command      string
	contextDir   string
	flowFilename string
	// logger is an optional that the final log results are dumped to if set.
	logger *zap.Logger
	// disableCleanup for app.Config.
	disableCleanup bool
}

func run(t *testing.T, config config) error {
	// Assert that the context dir and flow file exist.
	_, err := os.Stat(config.contextDir)
	require.NoError(t, err, "stat context dir should not fail")
	_, err = os.Stat(config.flowFilename)
	require.NoError(t, err, "stat flow file should not fail")

	logger, records := zaprec.NewRecorder(zap.DebugLevel)
	t.Cleanup(func() {
		if t.Failed() {
			logger, _ = logging.NewLogger(zap.DebugLevel)
			records.DumpToLogger(logger)
		}
	})
	if config.logger != nil {
		defer records.DumpToLogger(config.logger)
	}

	err = app.Run(context.Background(), logger, nil, app.Config{
		EngineType:     container.EngineTypeDocker,
		Command:        config.command,
		ContextDir:     config.contextDir,
		FlowFilename:   config.flowFilename,
		DisableCleanup: config.disableCleanup,
	})
	if err != nil {
		mehlog.LogToLevel(logger, zap.ErrorLevel, err)
	}
	return err
}

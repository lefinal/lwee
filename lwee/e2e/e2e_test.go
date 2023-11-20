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
	"path"
	"strings"
	"testing"
)

type config struct {
	command      string
	contextDir   string
	flowFilename string
	// logger is an optional that the final log results are dumped to if set.
	logger *zap.Logger
	// disableCleanup for app.LWEEConfig.
	disableCleanup          bool
	skipFileExistenceChecks bool
}

func run(t *testing.T, config config) error {
	require.False(t, strings.HasPrefix(path.Clean(config.flowFilename), path.Clean(config.contextDir)),
		"broken test. if the flow filename is not absolute, the context dir will be used as base.")
	// Assert that the context dir and flow file exist.
	if !config.skipFileExistenceChecks {
		_, err := os.Stat(config.contextDir)
		require.NoError(t, err, "stat context dir should not fail")
		if path.IsAbs(config.flowFilename) {
			_, err = os.Stat(config.flowFilename)
			require.NoError(t, err, "stat flow file should not fail")
		} else {
			_, err = os.Stat(path.Join(config.contextDir, config.flowFilename))
			require.NoError(t, err, "stat flow file should not fail")
		}
	}

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

	cliArgs := make([]string, 0)
	cliArgs = append(cliArgs, "lwee-e2e-test")
	cliArgs = append(cliArgs, "--engine", string(container.EngineTypeDocker))
	if config.contextDir != "" {
		cliArgs = append(cliArgs, "--dir", config.contextDir)
	}
	if config.flowFilename != "" {
		cliArgs = append(cliArgs, "--flow", config.flowFilename)

	}
	if config.disableCleanup {
		cliArgs = append(cliArgs, "--no-cleanup")
	}
	cliArgs = append(cliArgs, config.command)

	err := app.RunCLI(context.Background(), logger, cliArgs)
	if err != nil {
		mehlog.LogToLevel(logger, zap.ErrorLevel, err)
	}
	return err
}

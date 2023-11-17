package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"github.com/lefinal/lwee/lwee/app"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/waitforterminate"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"os"
	"strings"
)

const (
	envContainerEngine = "LWEE_CONTAINER_ENGINE"
)

//go:embed help.txt
var helpText string

const defaultEngineType = container.EngineTypeDocker

type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	return strings.Join(*f, ", ")
}

func (f *stringSliceFlag) Set(s string) error {
	*f = append(*f, s)
	return nil
}

func main() {
	err := waitforterminate.Run(run)
	if err != nil {
		mehlog.Log(logging.RootLogger(), err)
		_ = logging.RootLogger().Sync()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Parse flags.
	verboseFlag := flag.Bool("v", false, "Enables debug log output.")
	flowFilenameFlag := flag.String("f", "", "Flow file to use.")
	projectDirFlag := flag.String("dir", "./", "Project directory to use.")
	disableCleanupFlag := flag.Bool("no-cleanup", false, "When set, keeps temporary files and omits container cleanup.")
	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n", os.Args[0])
		fmt.Println()
		fmt.Println(helpText)
		fmt.Println("Arguments:")
		flag.PrintDefaults()
	}
	flag.Parse()
	// Set up logging.
	logLevel := zap.InfoLevel
	if *verboseFlag {
		logLevel = zap.DebugLevel
	}
	logger, err := logging.NewLogger(logLevel)
	if err != nil {
		return meh.Wrap(err, "new logger", meh.Details{"log_level": logLevel})
	}
	defer func() { _ = logger.Sync() }()
	logging.SetLogger(logger)
	// Set container engine.
	engineType, ok := os.LookupEnv(envContainerEngine)
	if !ok {
		engineType = string(defaultEngineType)
	}
	// Use provided command name.
	var commandName string
	if len(os.Args) > 1 {
		commandName = os.Args[1]
	}
	appInput := &input.Stdin{}
	err = app.Run(ctx, logger, appInput, app.Config{
		EngineType:     container.EngineType(engineType),
		Command:        commandName,
		FlowFilename:   *flowFilenameFlag,
		ContextDir:     *projectDirFlag,
		DisableCleanup: *disableCleanupFlag,
	})
	return err
}

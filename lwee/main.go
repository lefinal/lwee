package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"github.com/lefinal/lwee/lwee/app"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/shared-go/waitforterminate"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"os"
)

//go:embed help.txt
var helpText string

func main() {
	err := waitforterminate.Run(run)
	if err != nil {
		mehlog.Log(logging.RootLogger(), meh.Wrap(err, "run", nil))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Parse flags.
	verboseFlag := flag.Bool("v", false, "Enables debug log output.")
	nodeModeFlag := flag.Bool("node", false, "Starts LWEE in node-mode and listens on --listen for incoming connections.")
	listenFlag := flag.String("listen", ":17733", "Address to listen on in node-mode.")
	flowFilenameFlag := flag.String("f", app.DefaultFlowFilename, "Flow file to use.")
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
	// Extract context dir.
	command := ""
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	// Extract flow context dir.
	flowContextDir := ""
	if len(os.Args) > 2 {
		flowContextDir = os.Args[len(os.Args)-1]
	}
	go func() {
		input.Consume(ctx)
	}()
	// Run app.
	err = app.Run(ctx, logger, app.Config{
		Command:      app.Command(command),
		NodeMode:     *nodeModeFlag,
		ListenAddr:   *listenFlag,
		FlowFilename: *flowFilenameFlag,
		ContextDir:   flowContextDir,
	})
	return err
}

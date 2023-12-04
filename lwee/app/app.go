// Package app provides CLI functionality for LWEE.
package app

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/input"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lwee"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"os"
	"path"
	"runtime/debug"
	"time"
)

type commandOptions struct {
	Logger     *zap.Logger
	Input      input.Input
	Locator    *locator.Locator
	LWEEConfig lwee.Config
}

type buildSettings struct {
	revision string
	time     time.Time
}

func parseBuildSettings() buildSettings {
	buildInfo, _ := debug.ReadBuildInfo()
	settings := buildSettings{}
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			settings.revision = setting.Value
		case "vc.time":
			settings.time, _ = time.Parse(time.RFC3339, setting.Value)
		}
	}
	return settings
}

type cliOptions struct {
	verbose             bool
	containerEngineType string
	contextDir          string
	flowFilename        string
	disableCleanup      bool
}

// RunCLI the app as CLI. If the given logger is not nil, it will be used instead
// of creating a new one with provided flags.
func RunCLI(ctx context.Context, logger *zap.Logger, args []string) error {
	const defaultFlowFilename = "flow.yaml"

	buildSettings := parseBuildSettings()

	var cliOpts cliOptions
	var commandOpts commandOptions
	commandOpts.Logger = zap.NewNop()

	cliApp := &cli.App{
		Name:    "lwee",
		Usage:   "Lightweight Workflow Execution Engine",
		Version: "", // Don't set due to -v flag.
		Authors: []*cli.Author{
			{
				Name:  "Lennart Altenhof",
				Email: "l.f.altenhof@gmail.com",
			},
		},
		ExtraInfo: func() map[string]string {
			return map[string]string{
				"version":      buildSettings.revision,
				"version from": buildSettings.time.Format(time.DateTime),
			}
		},
		Compiled: buildSettings.time,

		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "verbose",
				Aliases:     []string{"v"},
				Usage:       "Enabled debug log output.",
				EnvVars:     []string{"LWEE_VERBOSE"},
				Required:    false,
				Destination: &cliOpts.verbose,
			},
			&cli.StringFlag{
				Name:        "dir",
				Usage:       "Use `DIR` as project directory. If not set, the working directory and its parents will be searched for a project instead.",
				Required:    false,
				Destination: &cliOpts.contextDir,
			},
			&cli.StringFlag{
				Name:        "flow",
				Aliases:     []string{"f"},
				Usage:       "The `FILENAME` of the flow file. If a non-absolute path is provided, the project directory will be used as base.",
				Value:       defaultFlowFilename,
				Required:    false,
				Destination: &cliOpts.flowFilename,
			},
			&cli.BoolFlag{
				Name:        "no-cleanup",
				Usage:       "If set, temporary files and containers will not be cleaned up.",
				EnvVars:     []string{"LWEE_NO_CLEANUP"},
				Required:    false,
				Destination: &cliOpts.disableCleanup,
			},
			&cli.StringFlag{
				Name:        "engine",
				Usage:       "`ENGINE` is the container engine to use. Valid values are 'docker' and 'podman'.",
				Value:       string(container.EngineTypeDocker),
				EnvVars:     []string{"LWEE_ENGINE"},
				Destination: &cliOpts.containerEngineType,
			},
		},

		Commands: []*cli.Command{
			{
				Name:    "run",
				Aliases: []string{"r"},
				Usage:   "Runs the specified workflow.",
				Before:  assureLWEEProject(&commandOpts),
				Action: func(c *cli.Context) error {
					return meh.NilOrWrap(commandRun(c.Context, commandOpts), "command run", nil)
				},
			},
			{
				Name:    "verify",
				Aliases: []string{"v"},
				Usage:   "Verifies the specified workflow.",
				Before:  assureLWEEProject(&commandOpts),
				Action: func(c *cli.Context) error {
					return meh.NilOrWrap(commandVerify(c.Context, commandOpts), "command verify", nil)
				},
			},
			{
				Name:    "create-action",
				Aliases: []string{"ca"},
				Usage:   "Creates a new project action in the project.",
				Before:  assureLWEEProject(&commandOpts),
				Action: func(c *cli.Context) error {
					return meh.NilOrWrap(commandCreateProjectAction(c.Context, commandOpts), "command create-action", nil)
				},
			},
			{
				Name:  "init",
				Usage: "Initializes an empty LWEE project in the current directory.",
				Action: func(c *cli.Context) error {
					return meh.NilOrWrap(commandInit(c.Context, commandOpts), "command init", nil)
				},
			},
			{
				Name:  "version",
				Usage: "Prints the current LWEE version.",
				Action: func(c *cli.Context) error {
					fmt.Println(buildSettings.revision)
					return nil
				},
			},
		},

		EnableBashCompletion: true,

		Before: func(c *cli.Context) error {
			var err error
			// Set up logging.
			if logger != nil {
				commandOpts.Logger = logger
			} else {
				logLevel := zap.InfoLevel
				if cliOpts.verbose {
					logLevel = zap.DebugLevel
				}
				logger, err := logging.NewLogger(logLevel)
				if err != nil {
					return meh.Wrap(err, "new logger", meh.Details{"log_level": logLevel})
				}
				defer func() { _ = logger.Sync() }()
				logging.SetLogger(logger)
				commandOpts.Logger = logger
				logger.Debug("applied log level", zap.String("log_level", logLevel.String()))
			}
			// Set container engine.
			commandOpts.LWEEConfig.ContainerEngineType = container.EngineType(cliOpts.containerEngineType)
			// Set up input.
			commandOpts.Input = &input.Stdin{}
			// Set up locator.
			searchContextDirInParents := !c.Args().Present() || (c.Args().First() != "init" && c.Args().First() != "version")
			commandOpts.Locator, err = newLocator(commandOpts.Logger, cliOpts.contextDir, cliOpts.flowFilename, searchContextDirInParents)
			if err != nil {
				return meh.Wrap(err, "new locator", nil)
			}

			commandOpts.LWEEConfig.DisableCleanup = cliOpts.disableCleanup
			return nil
		},

		Action: func(c *cli.Context) error {
			if c.Args().Present() {
				// Unknown command.
				_, _ = fmt.Fprintf(c.App.Writer, "unsupported command: %s\n\n", c.Args().First())
				cli.ShowAppHelpAndExit(c, 1)
				return nil
			}
			// No command provided. Offer selection and run manually.
			commandNames := make([]string, 0)
			for _, command := range c.App.Commands {
				commandNames = append(commandNames, command.Name)
			}
			selectedCommandIndex, _, err := commandOpts.Input.RequestSelection(ctx, "No command provided. Select one", commandNames)
			if err != nil {
				return meh.Wrap(err, "request selection due to missing command", nil)
			}
			selectedCommand := c.App.Commands[selectedCommandIndex]
			// Run the command.
			return selectedCommand.Run(c)
		},
		Metadata:               nil,
		UseShortOptionHandling: false,
		Suggest:                true,
	}

	start := time.Now()
	defer func() {
		commandOpts.Logger.Debug("shutdown", zap.Duration("total_command_execution_time", time.Since(start)))
	}()
	go func() {
		<-ctx.Done()
		commandOpts.Logger.Debug("shutdown initiated")
	}()

	return cliApp.RunContext(ctx, args)
}

func newLWEEWithOptions(options commandOptions) (*lwee.LWEE, error) {
	flowFile, err := lweeflowfile.FromFile(options.Locator.FlowFilename())
	if err != nil {
		return nil, meh.Wrap(err, "read flow file", meh.Details{"flow_filename": options.Locator.FlowFilename()})
	}
	created, err := lwee.New(options.Logger, flowFile, options.Locator, options.LWEEConfig)
	if err != nil {
		return nil, meh.Wrap(err, "new lwee", meh.Details{"config": options.LWEEConfig})
	}
	return created, nil
}

func assureLWEEProject(appOptions *commandOptions) func(c *cli.Context) error {
	return func(c *cli.Context) error {
		err := appOptions.Locator.AssureLWEEProject()
		if err != nil {
			return meh.Wrap(err, "assure lwee project", nil)
		}
		return nil
	}
}

func newLocator(logger *zap.Logger, contextDir string, flowFilename string, searchInParents bool) (*locator.Locator, error) {
	// If the context directory is not set, try to find it in the current directory
	// or any parent.
	if contextDir == "" {
		logger.Debug("no context dir provided")
		currentWorkingDirectory, err := os.Getwd()
		if err != nil {
			return nil, meh.NewInternalErrFromErr(err, "get current working directory", nil)
		}
		if searchInParents {
			logger.Debug("try to locate context dir in working directory or parents", zap.String("workdir", currentWorkingDirectory))
			contextDir, err = locator.FindContextDir(currentWorkingDirectory)
			if err != nil {
				return nil, meh.NewBadInputErrFromErr(err, "find context dir", meh.Details{"start_dir": currentWorkingDirectory})
			}
			logger.Debug("found context dir", zap.String("context_dir", contextDir))
		} else {
			logger.Debug("use current working directory as context dir", zap.String("workdir", currentWorkingDirectory))
			contextDir = currentWorkingDirectory
		}
	}
	if !path.IsAbs(flowFilename) {
		flowFilename = path.Join(contextDir, flowFilename)
	}
	logger.Debug("set up locator", zap.String("context_dir", contextDir), zap.String("flow_filename", flowFilename))
	appLocator, err := locator.New(contextDir, flowFilename)
	if err != nil {
		return nil, meh.Wrap(err, "new locator", meh.Details{
			"context_dir":   contextDir,
			"flow_filename": flowFilename,
		})
	}
	return appLocator, nil
}

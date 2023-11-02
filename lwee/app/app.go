package app

import (
	"context"
	"fmt"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
)

type Config struct {
	Command      Command
	NodeMode     bool
	ListenAddr   string
	FlowFilename string
	ContextDir   string
}

type Command string

const (
	CommandInit   Command = "init"
	CommandVerify Command = "verify"
)

const DefaultFlowFilename = "flow.yaml"

func Run(ctx context.Context, logger *zap.Logger, config Config) error {
	if config.NodeMode {
		fmt.Println(config.ListenAddr)
		panic("implement me")
	} else {
		err := runMaster(ctx, logger, config)
		if err != nil {
			return meh.Wrap(err, "run master", nil)
		}
	}
	return nil
}

func runMaster(ctx context.Context, logger *zap.Logger, config Config) error {
	switch config.Command {
	case CommandInit:
		err := commandInit(ctx, logger, config)
		if err != nil {
			return meh.Wrap(err, "init", nil)
		}
	case CommandVerify:
		err := commandVerify(ctx, logger, config)
		if err != nil {
			return meh.Wrap(err, "verify", nil)
		}
	case "":
		return meh.NewBadInputErr("missing command", nil)
	default:
		return meh.NewBadInputErr(fmt.Sprintf("unsupported command: %v", config.Command), nil)
	}
	return nil
}

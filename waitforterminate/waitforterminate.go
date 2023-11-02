// Package waitforterminate is used for waiting until the application is
// instructed to exit via signals.
package waitforterminate

import (
	"context"
	"github.com/lefinal/meh"
	"os"
	"os/signal"
	"syscall"
)

// Runnable that runs until the given context is done.
type Runnable func(ctx context.Context) error

// Run the given Runnable until a syscall.SIGINT or syscall.SIGTERM is received.
func Run(runnable Runnable) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		Wait(ctx)
	}()
	err := runnable(ctx)
	if err != nil {
		return meh.Wrap(err, "run", nil)
	}
	return nil
}

// Wait until a terminate signal is received or the given context is done.
func Wait(ctx context.Context) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-signals:
	}
}

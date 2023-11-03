// Package waitforterminate is used for waiting until the application is
// instructed to exit via signals.
package waitforterminate

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Wait until a terminate-signal is received or the given context is done.
func Wait(ctx context.Context) {
	<-Lifetime(ctx).Done()
}

// Lifetime returns a context that is done when a terminate-signal is received or
// the given context is done.
func Lifetime(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		defer cancel()
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-ctx.Done():
		case <-signals:
		}
	}()
	return ctx
}

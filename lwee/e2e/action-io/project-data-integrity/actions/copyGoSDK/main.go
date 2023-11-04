package main

import (
	"context"
	"github.com/lefinal/lwee/go-sdk/lweeclient"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
)

func main() {
	logger, err := lweeclient.NewLogger(zap.DebugLevel)
	if err != nil {
		panic(err)
	}
	client := lweeclient.New(lweeclient.Options{
		Logger: logger,
	})
	logger = logger.Named("main")
	inputStream, err := client.RequestInputStream("in")
	if err != nil {
		panic(err)
	}
	outputStream, err := client.ProvideOutputStream("out")
	if err != nil {
		panic(err)
	}

	eg, ctx := errgroup.WithContext(context.Background())

	eg.Go(func() error {
		err := client.Serve()
		if err != nil {
			return meh.Wrap(err, "serve", nil)
		}
		return nil
	})

	eg.Go(func() error {
		defer outputStream.Close(nil)
		logger.Debug("wait for input open")
		err := inputStream.WaitForOpen(ctx)
		if err != nil {
			return meh.Wrap(err, "wait for input stream open", nil)
		}
		logger.Debug("open output stream")
		err = outputStream.Open()
		if err != nil {
			return meh.Wrap(err, "notify output stream open", nil)
		}
		logger.Debug("copy data from input to output")
		_, err = io.Copy(outputStream, inputStream)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "copy data", nil)
		}
		logger.Debug("data copied")
		return nil
	})

	err = eg.Wait()
	if err != nil {
		panic(err)
	}
}

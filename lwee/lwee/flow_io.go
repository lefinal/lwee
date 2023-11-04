package lwee

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"io"
	"os"
	"path"
	"time"
)

func (lwee *LWEE) registerFlowInput(ctx context.Context, inputName string, flowInput any) error {
	sourceName := fmt.Sprintf("flow.in.%s", inputName)
	sourceProvider, err := lwee.ioSupplier.RegisterSourceProvider(sourceName, "flow.out",
		fmt.Sprintf("flow.in.%s", logging.WrapName(inputName)))
	sourceLogger := lwee.logger.Named("flow-input").Named(logging.WrapName(inputName))
	if err != nil {
		return meh.Wrap(err, fmt.Sprintf("register source provider for source %q", sourceName), nil)
	}
	switch flowInput := flowInput.(type) {
	case lweeflowfile.FlowInputFile:
		// Simply open the file and forward it.
		filename := path.Join(lwee.Locator.ContextDir(), flowInput.Filename)
		err = assureFileExists(filename)
		if err != nil {
			return meh.Wrap(err, "check file", meh.Details{"filename": filename})
		}
		go func() {
			err := provideFileSource(ctx, filename, sourceProvider)
			if err != nil {
				mehlog.Log(sourceLogger, meh.Wrap(err, "provide file source", meh.Details{"filename": filename}))
				return
			}
		}()
	default:
		return meh.NewBadInputErr(fmt.Sprintf("unsupported flow input type: %T", flowInput), nil)
	}
	return nil
}

func (lwee *LWEE) registerFlowOutput(_ context.Context, outputName string, flowOutput any) (flowOutput, error) {
	var sourceName string
	var sourceHandler func(ctx context.Context, logger *zap.Logger, source io.Reader) error
	// Set handlers based on output type.
	switch flowOutput := flowOutput.(type) {
	case lweeflowfile.FlowOutputFile:
		sourceName = flowOutput.Source
		sourceHandler = func(ctx context.Context, logger *zap.Logger, source io.Reader) error {
			filename := path.Join(lwee.Locator.ContextDir(), flowOutput.Filename)
			err := os.MkdirAll(path.Dir(filename), 0760)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "mkdir all for output file", meh.Details{"dir": path.Dir(filename)})
			}
			f, err := os.Create(filename)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "create output file", meh.Details{"filename": flowOutput.Filename})
			}
			defer func() { _ = os.Chtimes(filename, time.Now().Local(), time.Now().Local()) }()
			defer func() { _ = f.Close() }()
			n, err := io.Copy(f, source)
			if err != nil {
				return meh.Wrap(err, "write output file", nil)
			}
			logger.Debug("output file written",
				zap.String("bytes_written", logging.FormatByteCountDecimal(n)),
				zap.String("filename", filename))
			return nil
		}
	}
	// Handle source.
	if sourceName == "" {
		return nil, meh.NewInternalErr("no source name extracted", meh.Details{"flow_output_typ": fmt.Sprintf("%T", flowOutput)})
	}
	if sourceHandler == nil {
		return nil, meh.NewInternalErr("no source handler set", meh.Details{"flow_output_type": fmt.Sprintf("%T", flowOutput)})
	}
	source := lwee.ioSupplier.RequestSource(sourceName, "flow.in", fmt.Sprintf("flow.out.%s", logging.WrapName(outputName)))
	return func(ctx context.Context) error {
		logger := lwee.logger.Named("flow-output").Named(logging.WrapName(outputName)).
			With(zap.String("source_name", sourceName))
		// Wait for source opened.
		logger.Debug("wait for source opened")
		select {
		case <-ctx.Done():
			return meh.NewInternalErrFromErr(ctx.Err(), "wait for source opened", meh.Details{"source_name": sourceName})
		case <-source.Open:
		}
		logger.Debug("source open")
		defer func() { _ = source.Reader.Close() }()
		err := sourceHandler(ctx, logger, source.Reader)
		if err != nil {
			return meh.Wrap(err, fmt.Sprintf("handle flow output %q", outputName), meh.Details{"source_name": sourceName})
		}
		logger.Debug("output done")
		return nil
	}, nil
}

func assureFileExists(filename string) error {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "read file info", nil)
	}
	if fileInfo.IsDir() {
		return meh.NewBadInputErr("file is a directory", nil)
	}
	return nil
}

func provideFileSource(ctx context.Context, filename string, sourceProvider actionio.SourceWriter) error {
	defer func() { _ = sourceProvider.Writer.Close() }()
	f, err := os.Open(filename)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "open source file", meh.Details{"filename": filename})
	}
	defer func() { _ = f.Close() }()
	select {
	case <-ctx.Done():
	case sourceProvider.Open <- struct{}{}:
	}
	_, err = io.Copy(sourceProvider.Writer, f)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stream file as source", meh.Details{"filename": filename})
	}
	return nil
}

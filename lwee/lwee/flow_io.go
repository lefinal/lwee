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
			err := provideFlowFileSource(ctx, filename, sourceProvider)
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
	var sourceHandler func(ctx context.Context, logger *zap.Logger, source io.Reader, availableOptimizations *actionio.AvailableOptimizations) error
	// Set handlers based on the output type.
	switch flowOutput := flowOutput.(type) {
	case lweeflowfile.FlowOutputFile:
		sourceName = flowOutput.Source
		sourceHandler = func(ctx context.Context, logger *zap.Logger, source io.Reader, availableOptimizations *actionio.AvailableOptimizations) error {
			filename := flowOutput.Filename
			if !path.IsAbs(filename) {
				filename = path.Join(lwee.Locator.ContextDir(), flowOutput.Filename)
			}
			// If a file is available, we simply move it.
			if availableOptimizations.Filename != "" {
				logger.Debug("filename optimization available. moving file.",
					zap.String("src", availableOptimizations.Filename),
					zap.String("dst", filename))
				err := actionio.ApplyFileCopyOptimization(availableOptimizations.Filename, filename)
				if err == nil {
					availableOptimizations.SkipTransmission()
					return nil
				}
				// Failed.
				mehlog.LogToLevel(logger, zap.DebugLevel, meh.Wrap(err, "move file", meh.Details{
					"src": availableOptimizations.Filename,
					"dst": filename,
				}))
				logger.Debug("copy source regularly due to failed move")
			}
			// Copy file.
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
		// Wait for the source being opened.
		logger.Debug("wait for source opened")
		availableOptimizations, err := source.WaitForOpen(ctx)
		if err != nil {
			return meh.Wrap(err, "wait for source opened", nil)
		}
		logger.Debug("source open")
		defer func() { _ = source.Reader.Close() }()
		err = sourceHandler(ctx, logger, source.Reader, availableOptimizations)
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

func provideFlowFileSource(ctx context.Context, filename string, sourceProvider actionio.SourceWriter) error {
	defer func() { _ = sourceProvider.Writer.Close() }()
	f, err := os.Open(filename)
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "open source file", meh.Details{"filename": filename})
	}
	defer func() { _ = f.Close() }()
	// Open. However, we do not provide an alternative access to the file as this
	// might lead to optimizations where the file is moved instead of being copied.
	// As we want to keep flow inputs, this is not an option here.
	select {
	case <-ctx.Done():
	case sourceProvider.Open <- actionio.AlternativeSourceAccess{}:
	}
	_, err = io.Copy(sourceProvider.Writer, f)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "stream file as source", meh.Details{
			"source_name": sourceProvider.Name,
			"filename":    filename,
		})
	}
	return nil
}

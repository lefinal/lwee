package lweeclient

import (
	"bufio"
	"context"
	"fmt"
	"github.com/lefinal/lwee/go-sdk/waitforterminate"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"sync"
	"time"
)

const EnvDebug = "LWEE_DEBUG"
const DefaultOutputBufferSize = 16 * 1024 * 1024 // 16MB

type InputReader interface {
	WaitForOpen(ctx context.Context) error
	io.Reader
}

type OutputWriter interface {
	io.Writer
	Open() error
	Close(err error)
}

type Client interface {
	Lifetime() context.Context
	RequestInputStream(streamName string) (InputReader, error)
	ProvideOutputStream(streamName string) (OutputWriter, error)
	// Go is a helper method for allowing simplified error handling and usage of the
	// client's context. Behavior is similar to the one of the well-known errgroup.
	// The given function will be run concurrently. The provided context is the
	// client's lifetime context. If a non-nil error is returned, the client is shut
	// down as the operation is considered failed. Call this prior to Serve.
	Go(func(ctx context.Context) error)
	// Serve the HTTP API for LWEE. This will block until LWEE instructs the client
	// to shut down and all goroutines from Go are done.
	Serve() error
}

type inputStreamState string

const (
	inputStreamStateWaitForOpen inputStreamState = "wait-for-open"
	inputStreamStateOpen        inputStreamState = "open"
	inputStreamStateDone        inputStreamState = "done"
)

type inputStream struct {
	logger          *zap.Logger
	readerForApp    io.Reader
	writerForServer io.WriteCloser
	state           inputStreamState
	stateCond       *sync.Cond
}

func newInputStream(logger *zap.Logger) *inputStream {
	pipeReader, pipeWriter := io.Pipe()
	return &inputStream{
		logger:          logger,
		readerForApp:    pipeReader,
		writerForServer: pipeWriter,
		state:           inputStreamStateWaitForOpen,
		stateCond:       sync.NewCond(&sync.Mutex{}),
	}
}

func (input *inputStream) WaitForOpen(ctx context.Context) error {
	input.logger.Debug("wait for open")
	open := make(chan struct{})
	go func() {
		input.stateCond.L.Lock()
		for input.state == inputStreamStateWaitForOpen {
			input.stateCond.Wait()
		}
		input.stateCond.L.Unlock()
		select {
		case <-ctx.Done():
		case open <- struct{}{}:
		}
	}()
	select {
	case <-ctx.Done():
		return meh.NewInternalErrFromErr(ctx.Err(), "wait for state open", nil)
	case <-open:
	}
	input.logger.Debug("input now open")
	return nil
}

func (input *inputStream) Read(p []byte) (n int, err error) {
	return input.readerForApp.Read(p)
}

type outputStreamState string

const (
	outputStreamStateWaitForOpen outputStreamState = "wait-for-open"
	outputStreamStateOpen        outputStreamState = "open"
	outputStreamStateDone        outputStreamState = "done"
	outputStreamStateError       outputStreamState = "error"
)

type outputStream struct {
	logger               *zap.Logger
	writerForApp         io.Closer
	bufferedWriterForApp *bufio.Writer
	readerForServer      io.Reader
	state                outputStreamState
	writerErr            error
	// stateCond locks state and writerErr.
	stateCond *sync.Cond
}

func newOutputStream(logger *zap.Logger, bufferSize int) *outputStream {
	pipeReader, pipeWriter := io.Pipe()
	return &outputStream{
		logger:               logger,
		writerForApp:         pipeWriter,
		bufferedWriterForApp: bufio.NewWriterSize(pipeWriter, bufferSize),
		readerForServer:      pipeReader,
		state:                outputStreamStateWaitForOpen,
		stateCond:            sync.NewCond(&sync.Mutex{}),
	}
}

func (output *outputStream) Write(p []byte) (n int, err error) {
	return output.bufferedWriterForApp.Write(p)
}

func (output *outputStream) Open() error {
	output.stateCond.L.Lock()
	defer output.stateCond.L.Unlock()
	output.logger.Debug("open")
	if output.state != outputStreamStateWaitForOpen {
		return meh.NewBadInputErr("stream already open or done", meh.Details{"stream_state": output.state})
	}
	output.state = outputStreamStateOpen
	output.stateCond.Broadcast()
	return nil
}

func (output *outputStream) Close(err error) {
	_ = output.bufferedWriterForApp.Flush()
	output.stateCond.L.Lock()
	defer output.stateCond.L.Unlock()
	defer func() { _ = output.writerForApp.Close() }()
	output.logger.Debug("close")
	if output.state == outputStreamStateDone || output.state == outputStreamStateError {
		// Already closed.
		return
	}
	output.state = outputStreamStateDone
	if err != nil {
		output.state = outputStreamStateError
		output.writerErr = err
	}
	output.stateCond.Broadcast()
}

type client struct {
	logger                    *zap.Logger
	lifetime                  context.Context
	cancel                    context.CancelCauseFunc
	serving                   bool
	listenAddr                string
	outputBufferSize          int
	inputStreamsByStreamName  map[string]*inputStream
	outputStreamsByStreamName map[string]*outputStream
	goFns                     sync.WaitGroup
	m                         sync.Mutex
}

type Options struct {
	Logger           *zap.Logger
	ListenAddr       string
	OutputBufferSize int
}

func New(options Options) Client {
	const minBufferSize = 32 * 1024
	if options.OutputBufferSize <= minBufferSize {
		options.OutputBufferSize = DefaultOutputBufferSize
	}
	lifetime, cancel := context.WithCancelCause(waitforterminate.Lifetime(context.Background()))
	c := &client{
		lifetime:                  lifetime,
		cancel:                    cancel,
		logger:                    zap.NewNop(),
		serving:                   false,
		listenAddr:                ":17733",
		outputBufferSize:          options.OutputBufferSize,
		inputStreamsByStreamName:  make(map[string]*inputStream),
		outputStreamsByStreamName: make(map[string]*outputStream),
	}
	if options.Logger == nil {
		c.logger, _ = NewLogger(zap.DebugLevel)
	}
	if options.ListenAddr != "" {
		c.listenAddr = options.ListenAddr
	}
	return c
}

// NewLogger creates a new zap.Logger. Don't forget to call Sync() on the
// returned logged before exiting!
func NewLogger(level zapcore.Level) (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"stderr", "/tmp/lwee.log"}
	config.ErrorOutputPaths = []string{"stderr", "/tmp/lwee.log"}
	config.Encoding = "console"
	config.EncoderConfig = zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	config.OutputPaths = []string{"stdout"}
	config.Level = zap.NewAtomicLevelAt(level)
	config.DisableCaller = true
	config.DisableStacktrace = true
	logger, err := config.Build()
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "new zap production logger", meh.Details{"config": config})
	}
	mehlog.SetDefaultLevelTranslator(func(_ meh.Code) zapcore.Level {
		return zapcore.ErrorLevel
	})
	return logger, nil
}

func (c *client) Lifetime() context.Context {
	return c.lifetime
}

func (c *client) RequestInputStream(streamName string) (InputReader, error) {
	c.logger.Debug("request input stream", zap.String("stream_name", streamName))
	c.m.Lock()
	defer c.m.Unlock()
	if c.serving {
		return nil, meh.NewBadInputErr("already serving", nil)
	}
	if _, ok := c.inputStreamsByStreamName[streamName]; ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("duplicate request for input stream: %s", streamName), nil)
	}
	stream := newInputStream(c.logger.Named("input-stream").Named(streamName))
	c.inputStreamsByStreamName[streamName] = stream
	return stream, nil
}

func (c *client) ProvideOutputStream(streamName string) (OutputWriter, error) {
	c.logger.Debug("provide output stream", zap.String("stream_name", streamName))
	c.m.Lock()
	defer c.m.Unlock()
	if c.serving {
		return nil, meh.NewBadInputErr("already serving", nil)
	}
	if _, ok := c.outputStreamsByStreamName[streamName]; ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("duplicate providing of output stream: %s", streamName), nil)
	}
	stream := newOutputStream(c.logger.Named("output-stream").Named(streamName), c.outputBufferSize)
	c.outputStreamsByStreamName[streamName] = stream
	return stream, nil
}

// Go runs the provided function concurrently. It is similar to the well-known
// errgroup but passes the client's lifetime context. It also shuts down the
// client if the returned error is non-nil.
func (c *client) Go(fn func(ctx context.Context) error) {
	c.m.Lock()
	defer c.m.Unlock()
	c.goFns.Add(1)
	go func() {
		defer c.goFns.Done()
		start := time.Now()
		c.logger.Debug("start go-fn")
		defer c.logger.Debug("go-fn done", zap.Duration("took", time.Since(start)))
		err := fn(c.lifetime)
		c.m.Lock()
		defer c.m.Unlock()
		if err != nil {
			mehlog.Log(c.logger, meh.Wrap(err, "do-fn", nil))
			c.cancel(err)
			return
		}
	}()
}

func (c *client) Serve() error {
	c.m.Lock()
	listenAddr := c.listenAddr
	serving := c.serving
	if serving {
		defer c.m.Unlock()
		return meh.NewBadInputErr("already serving", nil)
	}
	serving = true
	c.m.Unlock()
	defer func() {
		c.m.Lock()
		c.serving = false
		if c.logger != nil {
			_ = c.logger.Sync()
		}
		c.m.Unlock()
	}()
	server := newServer(c.logger.Named("http"), listenAddr, c)
	err := server.serve(c.lifetime)
	if err != nil {
		return meh.Wrap(err, "serve", meh.Details{"listen_addr": listenAddr})
	}
	c.goFns.Wait()
	return nil
}

func (c *client) shutdown() {
	c.cancel(nil)
}

func (c *client) ioSummary() (ioSummary, error) {
	c.m.Lock()
	defer c.m.Unlock()
	c.logger.Debug("io summary requested")
	summary := ioSummary{
		requestedInputStreams: make([]string, 0),
		providedOutputStreams: make([]string, 0),
	}
	for streamName := range c.inputStreamsByStreamName {
		summary.requestedInputStreams = append(summary.requestedInputStreams, streamName)
	}
	for streamName := range c.outputStreamsByStreamName {
		summary.providedOutputStreams = append(summary.providedOutputStreams, streamName)
	}
	return summary, nil
}

func (c *client) readInputStream(streamName string, reader io.Reader) error {
	c.m.Lock()
	stream, ok := c.inputStreamsByStreamName[streamName]
	c.m.Unlock()
	if !ok {
		return meh.NewNotFoundErr(fmt.Sprintf("no requested input stream with this name: %s", streamName), nil)
	}
	// Open stream.
	stream.stateCond.L.Lock()
	if stream.state != inputStreamStateWaitForOpen {
		defer stream.stateCond.L.Unlock()
		return meh.NewBadInputErr(fmt.Sprintf("input stream in state %v", stream.state), nil)
	}
	stream.state = inputStreamStateOpen
	stream.stateCond.Broadcast()
	stream.stateCond.L.Unlock()
	defer func() {
		stream.stateCond.L.Lock()
		stream.state = inputStreamStateDone
		stream.stateCond.Broadcast()
		_ = stream.writerForServer.Close()
		stream.stateCond.L.Unlock()
	}()
	// Forward data.
	defer func() { _ = stream.writerForServer.Close() }()
	_, err := io.Copy(stream.writerForServer, reader)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "copy data", nil)
	}
	return nil
}

func (c *client) outputStreamByName(streamName string) (*outputStream, error) {
	c.m.Lock()
	defer c.m.Unlock()
	stream, ok := c.outputStreamsByStreamName[streamName]
	if !ok {
		return nil, meh.NewNotFoundErr(fmt.Sprintf("no provided output stream with this name: %s", streamName), nil)
	}
	return stream, nil
}

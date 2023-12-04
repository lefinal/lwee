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

const DefaultOutputBufferSize = 16 * 1024 * 1024 // 16MB
const DefaultListenAddr = ":17733"

// InputReader allows reading an input LWEE stream. Before reading, make sure to
// call WaitForOpen.
type InputReader interface {
	// WaitForOpen waits until the input is being opened or the given context is
	// done.
	WaitForOpen(ctx context.Context) error
	io.Reader
}

// OutputWriter allows writing an output LWEE stream. Before writing, make sure
// to call Open to notify receivers that output data is ready now. Then write
// your data as usual. When you are done, call Close.
type OutputWriter interface {
	io.Writer
	// Open notifies receivers that the output data is now ready to be transmitted.
	// Remember to call Open before writing any data!
	Open() error
	// Close the output stream. Remember to call this when all data is written.
	// Otherwise, LWEE will wait for more data to be transmitted. When the operation
	// and transmission were successful, call it with nil. Otherwise, you pass a
	// non-nil error to notify LWEE that execution failed.
	Close(err error)
}

// Client acts as an LWEE streams target by requesting input streams and
// providing output streams from and to LWEE. Create one with New and call Serve
// to launch an HTTP API that allows LWEE to communicate with the target.
//
// Note: In the future, more methods may be added to Client.
type Client interface {
	// Lifetime returns the Client's context that will be done once the client is
	// shutting down.
	Lifetime() context.Context
	// RequestInputStream requests the stream with the given name from LWEE. Call
	// this prior to Serve. Make sure to fully read from the stream once open.
	RequestInputStream(streamName string) (InputReader, error)
	// ProvideOutputStream provides the stream with the given name to LWEE. Call this
	// prior to Serve. Make sure to close the OutputWriter once you are done.
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

// WaitForOpen waits until the state is open or the given context is done.
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

// Read for implementing the io.Reader interface.
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

// client actually is more like a server because of communicating with LWEE by
// serving an HTTP API but there are many clients and only one LWEE instance, so
// we call this client. Create a new one with New and call Serve to start.
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

// Options for creating a Client with New.
type Options struct {
	// Optional logger to use. If not provided, a new debug logger will be created.
	Logger *zap.Logger
	// ListenAddr is the address under which to serve the HTTP API. Usually, you
	// should not overwrite this as LWEE expects the port from DefaultListenAddr.
	// However, this might be useful for debugging or development purposes.
	ListenAddr string
	// OutputBufferSize is the buffer size for output streams.
	OutputBufferSize int
}

// New creates a new Client with the given Options. Start it with Client.Serve.
func New(options Options) Client {
	const minBufferSize = 32 * 1024
	if options.OutputBufferSize <= minBufferSize {
		options.OutputBufferSize = DefaultOutputBufferSize
	}
	lifetime, cancel := context.WithCancelCause(waitforterminate.Lifetime(context.Background()))
	if options.Logger == nil {
		options.Logger, _ = NewLogger(zap.DebugLevel)
	}
	if options.ListenAddr == "" {
		options.ListenAddr = DefaultListenAddr
	}
	c := &client{
		lifetime:                  lifetime,
		cancel:                    cancel,
		logger:                    options.Logger,
		serving:                   false,
		listenAddr:                options.ListenAddr,
		outputBufferSize:          options.OutputBufferSize,
		inputStreamsByStreamName:  make(map[string]*inputStream),
		outputStreamsByStreamName: make(map[string]*outputStream),
	}
	return c
}

// NewLogger creates a new zap.Logger.
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

// Serve the HTTP API until a shutdown-request is received from LWEE.
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

// shutdown the client by cancelling lifetime. shutdown will not block. Instead,
// wait for Serve to return.
func (c *client) shutdown() {
	c.cancel(nil)
}

// ioSummary returns the ioSummary of input and output streams in the client.
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

// readInputStream reads data from the given reader and copies it to the
// inputStream.writerForServer. It also handles the state transition of the
// inputStream. If the inputStream state is not inputStreamStateWaitForOpen, it
// returns a meh.NewBadInputErr. If the inputStream is not found in the client's
// inputStreamsByStreamName map, it returns a meh.NewNotFoundErr. If there is an
// error while copying data, it returns a meh.NewInternalErrFromErr. Otherwise,
// it returns nil.
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

// outputStreamByName returns an output stream with the given name. If the stream
// is not found, it returns an error with code meh.ErrNotFound.
func (c *client) outputStreamByName(streamName string) (*outputStream, error) {
	c.m.Lock()
	defer c.m.Unlock()
	stream, ok := c.outputStreamsByStreamName[streamName]
	if !ok {
		return nil, meh.NewNotFoundErr(fmt.Sprintf("no provided output stream with this name: %s", streamName), nil)
	}
	return stream, nil
}

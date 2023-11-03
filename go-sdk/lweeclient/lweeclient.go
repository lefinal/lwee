package lweeclient

import (
	"bufio"
	"context"
	"fmt"
	"github.com/lefinal/lwee/go-sdk/waitforterminate"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
	"sync"
)

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
	Serve() error
}

type inputStreamState string

const (
	inputStreamStateWaitForOpen inputStreamState = "wait-for-open"
	inputStreamStateOpen        inputStreamState = "open"
	inputStreamStateDone        inputStreamState = "done"
)

type inputStream struct {
	readerForApp    io.Reader
	writerForServer io.WriteCloser
	state           inputStreamState
	stateCond       *sync.Cond
}

func newInputStream() *inputStream {
	pipeReader, pipeWriter := io.Pipe()
	return &inputStream{
		readerForApp:    pipeReader,
		writerForServer: pipeWriter,
		state:           inputStreamStateWaitForOpen,
		stateCond:       sync.NewCond(&sync.Mutex{}),
	}
}

func (input *inputStream) WaitForOpen(ctx context.Context) error {
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
	writerForApp         io.Closer
	bufferedWriterForApp *bufio.Writer
	readerForServer      io.Reader
	state                outputStreamState
	writerErr            error
	// stateCond locks state and writerErr.
	stateCond *sync.Cond
}

func newOutputStream(bufferSize int) *outputStream {
	pipeReader, pipeWriter := io.Pipe()
	return &outputStream{
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
	if options.Logger != nil {
		c.logger = options.Logger
	}
	if options.ListenAddr != "" {
		c.listenAddr = options.ListenAddr
	}
	return c
}

func (c *client) Lifetime() context.Context {
	return c.lifetime
}

func (c *client) RequestInputStream(streamName string) (InputReader, error) {
	c.m.Lock()
	defer c.m.Unlock()
	if c.serving {
		return nil, meh.NewBadInputErr("already serving", nil)
	}
	if _, ok := c.inputStreamsByStreamName[streamName]; ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("duplicate request for input stream: %s", streamName), nil)
	}
	stream := newInputStream()
	c.inputStreamsByStreamName[streamName] = stream
	return stream, nil
}

func (c *client) ProvideOutputStream(streamName string) (OutputWriter, error) {
	c.m.Lock()
	defer c.m.Unlock()
	if c.serving {
		return nil, meh.NewBadInputErr("already serving", nil)
	}
	if _, ok := c.outputStreamsByStreamName[streamName]; ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("duplicate providing of output stream: %s", streamName), nil)
	}
	stream := newOutputStream(c.outputBufferSize)
	c.outputStreamsByStreamName[streamName] = stream
	return stream, nil
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
		c.m.Unlock()
	}()
	server := newServer(c.logger.Named("http"), listenAddr, c)
	err := server.serve(c.lifetime)
	if err != nil {
		return meh.Wrap(err, "serve", meh.Details{"listen_addr": listenAddr})
	}
	return nil
}

func (c *client) shutdown() {
	c.cancel(nil)
}

func (c *client) ioSummary() (ioSummary, error) {
	c.m.Lock()
	defer c.m.Unlock()
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

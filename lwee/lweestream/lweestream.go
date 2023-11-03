package lweestream

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const connectCooldown = 200 * time.Millisecond

const DefaultTargetPort = "17733"

type Connector interface {
	RegisterStreamInputOffer(streamName string) error
	RegisterStreamOutputRequest(streamName string) error
	ConnectAndVerify(ctx context.Context, host string, port string) error
	WriteInputStream(ctx context.Context, streamName string, r io.Reader) error
	ReadOutputStream(ctx context.Context, streamName string, ready chan<- struct{}, writer io.Writer) error
	PipeIO(ctx context.Context) error
}

type connector struct {
	logger     *zap.Logger
	targetHost string
	targetPort string
	// offeredInputStreamsToTarget holds all input streams that are offered to the
	// target. The boolean value describes whether it is requested by the target or
	// if data to send will be discarded.
	offeredInputStreamsToTarget map[string]bool
	// expectedOutputStreamsFromTarget holds all output streams that are expected
	// from the target. The boolean value describes whether it is requested by LWEE
	// or if received data will be discarded.
	expectedOutputStreamsFromTarget map[string]bool
	streamInputsOutputsCond         *sync.Cond
	httpClient                      *http.Client
}

func NewConnector(logger *zap.Logger) Connector {
	return &connector{
		logger:                          logger,
		offeredInputStreamsToTarget:     make(map[string]bool),
		expectedOutputStreamsFromTarget: make(map[string]bool),
		streamInputsOutputsCond:         sync.NewCond(&sync.Mutex{}),
		httpClient:                      &http.Client{},
	}
}

func (c *connector) RegisterStreamInputOffer(streamName string) error {
	c.streamInputsOutputsCond.L.Lock()
	defer c.streamInputsOutputsCond.L.Unlock()
	defer c.streamInputsOutputsCond.Broadcast()
	if _, ok := c.offeredInputStreamsToTarget[streamName]; ok {
		return meh.NewBadInputErr(fmt.Sprintf("duplicate stream input: %s", streamName), nil)
	}
	c.offeredInputStreamsToTarget[streamName] = false
	return nil
}

func (c *connector) RegisterStreamOutputRequest(streamName string) error {
	c.streamInputsOutputsCond.L.Lock()
	defer c.streamInputsOutputsCond.L.Unlock()
	defer c.streamInputsOutputsCond.Broadcast()
	if _, ok := c.expectedOutputStreamsFromTarget[streamName]; ok {
		return meh.NewBadInputErr(fmt.Sprintf("duplicate stream output: %s", streamName), nil)
	}
	c.expectedOutputStreamsFromTarget[streamName] = false
	return nil
}

func (c *connector) ConnectAndVerify(ctx context.Context, host string, port string) error {
	c.targetHost = host
	c.targetPort = port
	// If no stream inputs/outputs are requested/provided, we can skip connecting to
	// the target.
	c.streamInputsOutputsCond.L.Lock()
	if len(c.offeredInputStreamsToTarget) == 0 && len(c.expectedOutputStreamsFromTarget) == 0 {
		c.streamInputsOutputsCond.L.Unlock()
		return nil
	}
	c.streamInputsOutputsCond.L.Unlock()
	// Assure target host and port set.
	if c.targetHost == "" {
		return meh.NewInternalErr("missing target host", nil)
	}
	if c.targetPort == "" {
		return meh.NewInternalErr("missing target port", nil)
	}
	// Request IO summary from target.
	reqUrl, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io")
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create url for connection attempt", meh.Details{
			"url":  c.targetHost,
			"port": c.targetPort,
		})
	}
	var response *http.Response
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "build http request", meh.Details{"url": reqUrl})
		}
		c.logger.Debug("request io summary from target", zap.String("req_url", reqUrl))
		response, err = c.httpClient.Do(req)
		if err != nil {
			// Request failed. We wait for the cooldown or the context being done and then
			// try again.
			c.logger.Debug("io summary request failed",
				zap.Error(err),
				zap.String("req_url", reqUrl),
				zap.Duration("retry_in", connectCooldown))
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "await connect cooldown", meh.Details{"cooldown": connectCooldown})
			case <-time.After(connectCooldown):
				continue
			}
		}
		break
	}
	// We got our response.
	defer func() { _ = response.Body.Close }()
	if response.StatusCode != http.StatusOK {
		return errorFromUnexpectedStatusCode(response)
	}

	var ioSummary struct {
		RequestedInputStreams []string `json:"requested_input_streams"`
		ProvidedOutputStreams []string `json:"provided_output_streams"`
	}
	err = json.NewDecoder(response.Body).Decode(&ioSummary)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "parse io summary response", nil)
	}
	c.logger.Debug("got io summary", zap.Any("io_summary", ioSummary))
	c.streamInputsOutputsCond.L.Lock()
	defer c.streamInputsOutputsCond.L.Unlock()
	defer c.streamInputsOutputsCond.Broadcast()
	// Assure all requested input streams from target are satisfied.
	for _, requestedInputStreamName := range ioSummary.RequestedInputStreams {
		_, ok := c.offeredInputStreamsToTarget[requestedInputStreamName]
		if !ok {
			availableInputStreams := make([]string, 0)
			for availableInputStreamName := range c.offeredInputStreamsToTarget {
				availableInputStreams = append(availableInputStreams, availableInputStreamName)
			}
			return meh.NewBadInputErr(fmt.Sprintf("target requested input stream %q but was not provided by lwee", requestedInputStreamName),
				meh.Details{"available_input_streams": availableInputStreams})
		}
		c.offeredInputStreamsToTarget[requestedInputStreamName] = true
	}
	// Assure all expected output streams are provided by the target.
	newExpectedOutputStreamsWithUsedIdentifier := make(map[string]bool)
	maps.Copy(c.expectedOutputStreamsFromTarget, newExpectedOutputStreamsWithUsedIdentifier)
	for expectedOutputStreamName := range c.expectedOutputStreamsFromTarget {
		found := false
		for _, actuallyProvidedOutputStreamName := range ioSummary.ProvidedOutputStreams {
			if expectedOutputStreamName == actuallyProvidedOutputStreamName {
				found = true
			}
		}
		if !found {
			actuallyProvidedOutputStreams := make([]string, 0)
			for _, actuallyProvidedOutputStreamName := range ioSummary.ProvidedOutputStreams {
				actuallyProvidedOutputStreams = append(actuallyProvidedOutputStreams, actuallyProvidedOutputStreamName)
			}
			return meh.NewBadInputErr(fmt.Sprintf("expected output stream %q was not provided by target", expectedOutputStreamName),
				meh.Details{"provided_output_streams": actuallyProvidedOutputStreams})
		}
		c.expectedOutputStreamsFromTarget[expectedOutputStreamName] = true
	}
	// Take note of remaining provided output streams that are not expected but
	// should be drained by the connector.
	for _, streamName := range ioSummary.ProvidedOutputStreams {
		_, ok := c.expectedOutputStreamsFromTarget[streamName]
		if !ok {
			c.expectedOutputStreamsFromTarget[streamName] = false
		}
	}
	return nil
}

// WriteInputStream forwards all data in a single HTTP request to the target.
func (c *connector) WriteInputStream(ctx context.Context, streamName string, r io.Reader) error {
	logger := c.logger.Named("stream").Named("lwee-to-target").Named(logging.WrapName(streamName))
	logger.Debug("write input stream")
	start := time.Now()
	defer func() {
		c.streamInputsOutputsCond.L.Lock()
		defer c.streamInputsOutputsCond.L.Unlock()
		defer c.streamInputsOutputsCond.Broadcast()
		_, ok := c.offeredInputStreamsToTarget[streamName]
		if !ok {
			c.streamInputsOutputsCond.L.Unlock()
			mehlog.Log(c.logger, meh.NewInternalErr("detected duplicate read for input stream", meh.Details{"stream_name": streamName}))
		}
		delete(c.offeredInputStreamsToTarget, streamName)
		logger.Debug("input stream written", zap.Duration("took", time.Since(start)))
	}()
	// Write data.
	reqUrl, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io/input", streamName)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqUrl, r)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqUrl})
	}
	logger.Debug("forward source data to stream", zap.String("req_url", reqUrl))
	response, err := c.httpClient.Do(req)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqUrl})
	}
	defer func() { _ = response.Body.Close }()
	if response.StatusCode != http.StatusOK {
		return errorFromUnexpectedStatusCode(response)
	}
	logger.Debug("source data written to stream")
	return nil
}

// ReadOutputStream reads stream output from the target by making HTTP requests
// and piping data according to the returned status code.
func (c *connector) ReadOutputStream(ctx context.Context, streamName string, ready chan<- struct{}, writer io.Writer) error {
	logger := c.logger.Named("stream").Named("target-to-lwee").Named(logging.WrapName(streamName))
	logger.Debug("read output stream")
	start := time.Now()
	defer func() {
		c.streamInputsOutputsCond.L.Lock()
		defer c.streamInputsOutputsCond.L.Unlock()
		defer c.streamInputsOutputsCond.Broadcast()
		_, ok := c.expectedOutputStreamsFromTarget[streamName]
		if !ok {
			mehlog.Log(c.logger, meh.NewInternalErr("detected duplicate read for output stream", meh.Details{"stream_name": streamName}))
			return
		}
		delete(c.expectedOutputStreamsFromTarget, streamName)
		logger.Debug("output stream read", zap.Duration("took", time.Since(start)))
	}()
	// Read data.
	reqUrl, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io/output", streamName)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	sourceOpened := false
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqUrl})
		}
		logger.Debug("request stream output from target", zap.String("req_url", reqUrl))
		response, err := c.httpClient.Do(req)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqUrl})
		}
		logger.Debug("got response", zap.String("status", response.Status), zap.Int("status_code", response.StatusCode))
		defer func() { _ = response.Body.Close() }()
		switch response.StatusCode {
		case http.StatusTooEarly:
			// Nothing to do. We simply repeat the request as we currently expect the target
			// to block until the output stream is ready. In the future we might introduce a
			// cooldown delay that will be applied after the second request.
		case http.StatusOK:
			// Forward data.
			if !sourceOpened {
				select {
				case <-ctx.Done():
					return meh.NewInternalErrFromErr(ctx.Err(), "notify source open", nil)
				case ready <- struct{}{}:
				}
				sourceOpened = true
			}
			logger.Debug("copy data from stream output")
			_, err = io.Copy(writer, response.Body)
			if err != nil {
				return meh.NewInternalErrFromErr(err, "copy data from stream output to source", nil)
			}
		case http.StatusNoContent:
			logger.Debug("stream output closed")
			return nil
		default:
			return errorFromUnexpectedStatusCode(response)
		}
	}
}

func (c *connector) PipeIO(ctx context.Context) error {
	// Defer target shutdown with timeout.
	defer func() {
		const timeoutDur = 3 * time.Second
		timeout, cancel := context.WithTimeout(context.Background(), timeoutDur)
		defer cancel()
		c.logger.Debug("shutdown target")
		err := c.shutdownTarget(timeout)
		if err != nil {
			mehlog.LogToLevel(c.logger, zap.DebugLevel, meh.Wrap(err, "shutdown target", nil))
			return
		}
	}()

	eg, egCtx := errgroup.WithContext(ctx)
	// Broadcast when either the context is done or we are finished in order to
	// unblock any goroutines that are waiting for the condition to notify.
	go func() {
		select {
		case <-ctx.Done():
		case <-egCtx.Done():
		}
		c.streamInputsOutputsCond.L.Lock()
		c.streamInputsOutputsCond.L.Unlock()
		c.streamInputsOutputsCond.Broadcast()
	}()
	// Drain unused output streams.
	eg.Go(func() error {
		err := c.drainUnusedOutputStreams(egCtx)
		if err != nil {
			return meh.Wrap(err, "drain unused output streams", nil)
		}
		return nil
	})
	// Wait until no more IO is expected.
	eg.Go(func() error {
		c.streamInputsOutputsCond.L.Lock()
		for len(c.offeredInputStreamsToTarget) > 0 || len(c.expectedOutputStreamsFromTarget) > 0 {
			c.streamInputsOutputsCond.Wait()
		}
		c.streamInputsOutputsCond.L.Unlock()
		return nil
	})

	err := eg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func (c *connector) shutdownTarget(ctx context.Context) error {
	reqUrl, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/shutdown")
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqUrl, nil)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqUrl})
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqUrl})
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errorFromUnexpectedStatusCode(response)
	}
	return nil
}

func (c *connector) drainUnusedOutputStreams(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	c.streamInputsOutputsCond.L.Lock()
	for streamName, isUsed := range c.expectedOutputStreamsFromTarget {
		streamName := streamName
		if isUsed {
			continue
		}
		// Drain it.
		eg.Go(func() error {
			defer func() {
				c.streamInputsOutputsCond.L.Lock()
				delete(c.expectedOutputStreamsFromTarget, streamName)
				c.streamInputsOutputsCond.L.Unlock()
				c.streamInputsOutputsCond.Broadcast()
			}()
			err := c.ReadOutputStream(ctx, streamName, make(chan struct{}, 1), io.Discard)
			if err != nil {
				return meh.Wrap(err, "discard unused output stream", meh.Details{"stream_name": streamName})
			}
			return nil
		})
	}
	c.streamInputsOutputsCond.L.Unlock()
	return eg.Wait()
}

func errorFromUnexpectedStatusCode(r *http.Response) error {
	errMessage, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		errMessage = []byte(fmt.Sprintf("<cannot read error message: %s>", readErr.Error()))
	}
	responseErr := meh.NewInternalErr(string(errMessage), nil)
	return meh.Wrap(responseErr, fmt.Sprintf("unexpected status code (%d)", r.StatusCode), meh.Details{
		"http_status":      r.Status,
		"http_status_code": r.StatusCode,
	})
}

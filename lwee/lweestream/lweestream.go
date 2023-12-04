// Package lweestream provides a Connector for handling LWEE streams.
package lweestream

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lefinal/lwee/lwee/actionio"
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

// connectCooldown is the cooldown to apply when trying to connect to the target.
const connectCooldown = 200 * time.Millisecond

// DefaultTargetPort is the default port that we expect the target to be
// listening on.
const DefaultTargetPort = "17733"

const defaultHTTPBufferSize = 1024 * 1024

// Connector provides communication with an LWEE streams target.
type Connector interface {
	// RegisterStreamInputOffer registers the stream with the given name as input
	// offer to the target.
	RegisterStreamInputOffer(streamName string) error
	// RegisterStreamOutputRequest requests the stream with the given name from the
	// target.
	RegisterStreamOutputRequest(streamName string) error
	// HasRegisteredIO describes whether stream inputs or outputs were registered.
	HasRegisteredIO() bool
	// ConnectAndVerify connects to the target and verifies that all IO expectations
	// are met. This means that all requested input streams by the target are
	// provided and all requested output streams by LWEE are provided by the target.
	ConnectAndVerify(ctx context.Context, host string, port string) error
	// WriteInputStream writes the given data to the input stream to the target with
	// the given name. Do not call this twice with the same stream name.
	WriteInputStream(ctx context.Context, streamName string, r io.Reader) error
	// ReadOutputStream waits until the output stream from the target opened. It will
	// then notify over the given channel and then copy all data to the given writer.
	// To not call this twice with the same stream name.
	ReadOutputStream(ctx context.Context, streamName string, ready chan<- actionio.AlternativeSourceAccess, writer io.Writer) error
	// PipeIO handles IO until all input and output streams are handled. It also
	// drains unused outputs from the target. Block until all streams are done.
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

// NewConnector creates a new Connector.
func NewConnector(logger *zap.Logger) Connector {
	netDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	httpRoundTripper := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           netDialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       defaultHTTPBufferSize,
		ReadBufferSize:        defaultHTTPBufferSize,
	}
	return &connector{
		logger:                          logger,
		offeredInputStreamsToTarget:     make(map[string]bool),
		expectedOutputStreamsFromTarget: make(map[string]bool),
		streamInputsOutputsCond:         sync.NewCond(&sync.Mutex{}),
		httpClient:                      &http.Client{Transport: httpRoundTripper},
	}
}

// RegisterStreamInputOffer registers an input stream offer to the target. It
// checks if the stream is already registered and returns an error if it is. It
// then adds the stream to the list of offered streams.
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

// RegisterStreamOutputRequest registers an output stream request to the target.
// It checks if the stream is already registered and returns an error if it is.
// It then adds the stream to the list of expected output streams.
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

// HasRegisteredIO checks if there are any registered input or output streams
// with the target. This allows skipping streams functionality if not required.
func (c *connector) HasRegisteredIO() bool {
	c.streamInputsOutputsCond.L.Lock()
	defer c.streamInputsOutputsCond.L.Unlock()
	if len(c.expectedOutputStreamsFromTarget) > 0 {
		return true
	}
	if len(c.offeredInputStreamsToTarget) > 0 {
		return true
	}
	return false
}

// ConnectAndVerify connects to the target and verifies the stream inputs and
// outputs. If no stream inputs/outputs are requested/provided, it will skip
// connecting to the target. It sets the target host and port, then requests the
// IO summary from the target. It checks the response status code and decodes the
// response body into the IO summary structure. It then verifies that all
// requested input streams from the target are satisfied and that all expected
// output streams are provided by the target. Finally, it takes note of any
// remaining provided output streams that are not expected but should be drained
// by the connector later in PipeIO. Example usage:
//
//	err := connector.ConnectAndVerify(ctx, "example.com", "8080")
//	if err != nil {
//		log.Fatal(err)
//	}
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
	reqURL, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io")
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create url for connection attempt", meh.Details{
			"url":  c.targetHost,
			"port": c.targetPort,
		})
	}
	var response *http.Response
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "build http request", meh.Details{"url": reqURL})
		}
		c.logger.Debug("request io summary from target", zap.String("req_url", reqURL))
		response, err = c.httpClient.Do(req)
		if err != nil {
			// Request failed. We wait for the cooldown or the context being done and then
			// try again.
			c.logger.Debug("io summary request failed",
				zap.Error(err),
				zap.String("req_url", reqURL),
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
	reqURL, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io/input", streamName)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, r)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqURL})
	}
	logger.Debug("forward source data to stream", zap.String("req_url", reqURL))
	response, err := c.httpClient.Do(req)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqURL})
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
func (c *connector) ReadOutputStream(ctx context.Context, streamName string, ready chan<- actionio.AlternativeSourceAccess, writer io.Writer) error {
	const cooldown = 200 * time.Millisecond

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
	reqURL, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/io/output", streamName)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	sourceOpened := false
	requestNum := -1
	// Use more complex closing of the response body so we can avoid large resource
	// consumption while waiting for the output to open.
	var response *http.Response
	defer func() {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
	}()
	for {
		requestNum++
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqURL})
		}
		logger.Debug("request stream output from target", zap.String("req_url", reqURL))
		response, err = c.httpClient.Do(req)
		if err != nil {
			return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqURL})
		}
		logger.Debug("got response", zap.String("status", response.Status), zap.Int("status_code", response.StatusCode))
		// Body closing is handled at the loop beginning and also deferred.
		switch response.StatusCode {
		case http.StatusAccepted:
			_ = response.Body.Close() // Avoid resource leak.
			// Output stream is not ready yet. Retry with cooldown. However, if this is our
			// first request, we skip the cooldown. The reason for this is that some clients
			// might simply block until the output stream is ready and then return 202. We
			// skip the first cooldown so that we can achieve near zero latencies.
			if requestNum == 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), fmt.Sprintf("cooldown due to status %d", response.StatusCode), nil)
			case <-time.After(cooldown):
			}
		case http.StatusOK:
			// Forward data.
			if !sourceOpened {
				select {
				case <-ctx.Done():
					return meh.NewInternalErrFromErr(ctx.Err(), "notify source open", nil)
				case ready <- actionio.AlternativeSourceAccess{}:
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

// PipeIO pipes input and output streams to and from the target. It first checks
// if there are any registered input and output streams. If not, it returns nil.
// PipeIO drains unused output streams and waits until no more IO is expected
// from the target. It also broadcasts a signal when either the context is done
// or when the error group's context is done to notify any goroutines that are
// waiting on the input/output conditions.
func (c *connector) PipeIO(ctx context.Context) error {
	if !c.HasRegisteredIO() {
		return nil
	}
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
	// Broadcast when either the context is done or we are finished to unblock any
	// goroutines that are waiting for the condition to notify.
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

// shutdownTarget sends a POST request with the /api/v1/shutdown endpoint to the
// target. It returns an error if the request fails or if the response status
// code is not 200.
func (c *connector) shutdownTarget(ctx context.Context) error {
	reqURL, err := url.JoinPath("http://", net.JoinHostPort(c.targetHost, c.targetPort), "/api/v1/shutdown")
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create request url", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "create http request", meh.Details{"req_url": reqURL})
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "do http request", meh.Details{"req_url": reqURL})
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errorFromUnexpectedStatusCode(response)
	}
	return nil
}

// drainUnusedOutputStreams drains unused output streams from the target. It
// checks each expected output stream, and if it is unused, it discards the data.
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
			err := c.ReadOutputStream(ctx, streamName, make(chan actionio.AlternativeSourceAccess, 1), io.Discard)
			if err != nil {
				return meh.Wrap(err, "discard unused output stream", meh.Details{"stream_name": streamName})
			}
			return nil
		})
	}
	c.streamInputsOutputsCond.L.Unlock()
	return eg.Wait()
}

// errorFromUnexpectedStatusCode creates an error from an unexpected HTTP status
// code. It reads the error message from the response body and wraps it with
// additional information. If reading the error message fails, it returns an
// error with a placeholder message. The returned error is wrapped with the
// specific error message and details.
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

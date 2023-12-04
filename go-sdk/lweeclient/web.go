package lweeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehhttp"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// handlerFunc is meant to be used with ginHandlerFunc.
type handlerFunc func(c *gin.Context) error

// ginHandlerFunc creates a gin.HandlerFunc from the given handlerFunc. The error
// returned is passed to logAndRespondError with the given zap.Logger.
func ginHandlerFunc(logger *zap.Logger, fn handlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Call handler.
		err := fn(c)
		if err != nil {
			logAndRespondError(logger, c, err)
			return
		}
	}
}

// logAndRespondError logs the given meh.Error and responds using the status code
// mapping set via mehhttp.HTTPStatusCode. The responding message will hold the
// error message. This is similar to mehgin.LogAndRespondError but uses the error
// message as body.
func logAndRespondError(logger *zap.Logger, c *gin.Context, e error) {
	// Add request details.
	e = meh.ApplyDetails(e, meh.Details{
		"http_req_url":         c.Request.URL.String(),
		"http_req_host":        c.Request.Host,
		"http_req_method":      c.Request.Method,
		"http_req_user_agent":  c.Request.UserAgent(),
		"http_req_remote_addr": c.Request.RemoteAddr,
	})
	mehlog.Log(logger, e)
	errMessage := e.Error()
	if errMarshalled, marshallErr := json.Marshal(e); marshallErr == nil {
		errMessage += " "
		errMessage += string(errMarshalled)
	}
	c.String(mehhttp.HTTPStatusCode(e), errMessage)
}

// requestDebugLogger logs requests on zap.DebugLevel to the given zap.Logger.
// The idea is based on gin.Logger.
func requestDebugLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		// Process request.
		c.Next()
		// Log results.
		logger.Debug("request",
			zap.Time("timestamp", start),
			zap.Duration("took", time.Now().Sub(start)),
			zap.String("path", c.Request.URL.Path),
			zap.String("raw_query", c.Request.URL.RawQuery),
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.Int("status_code", c.Writer.Status()),
			zap.String("error_message", c.Errors.ByType(gin.ErrorTypePrivate).String()),
			zap.Int("body_size", c.Writer.Size()),
			zap.String("user_agent", c.Request.UserAgent()))
	}
}

type ioSummary struct {
	requestedInputStreams []string
	providedOutputStreams []string
}

type serverHandler interface {
	shutdown()
	ioSummary() (ioSummary, error)
	readInputStream(streamName string, reader io.Reader) error
	outputStreamByName(streamName string) (*outputStream, error)
}

// server provides an HTTP API for usage with LWEE streams.
type server struct {
	logger     *zap.Logger
	listenAddr string
	handler    serverHandler
	engine     *gin.Engine
	httpServer *http.Server
}

// newServer creates a new server with routes being set up. Call server.serve to
// start the HTTP API.
func newServer(logger *zap.Logger, listenAddr string, handler serverHandler) *server {
	gin.SetMode(gin.ReleaseMode)
	s := &server{
		logger:     logger,
		listenAddr: listenAddr,
		handler:    handler,
		engine:     gin.New(),
	}
	// Setup server.
	s.engine.Use(requestDebugLogger(s.logger))
	s.engine.GET("/api/v1/io", ginHandlerFunc(s.logger, s.handleGetIO()))
	s.engine.POST("/api/v1/io/input/:streamName", ginHandlerFunc(s.logger, s.handleProvideInputStream()))
	s.engine.GET("/api/v1/io/output/:streamName", ginHandlerFunc(s.logger, s.handleRequestOutputStream()))
	s.engine.POST("/api/v1/shutdown", ginHandlerFunc(s.logger, s.handleShutdown()))
	return s
}

// serve the HTTP API until the given context is done.
func (s *server) serve(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:              s.listenAddr,
		Handler:           s.engine,
		ReadTimeout:       0,
		ReadHeaderTimeout: 0,
		WriteTimeout:      0,
		IdleTimeout:       0,
		MaxHeaderBytes:    0,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		ConnContext: func(_ context.Context, _ net.Conn) context.Context {
			return ctx
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		_ = s.httpServer.Shutdown(context.Background())
	}()
	s.logger.Debug("serving http", zap.String("listen_addr", s.httpServer.Addr))
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return meh.NewBadInputErrFromErr(err, "serve http", meh.Details{"addr": s.httpServer.Addr})
	}
	wg.Wait()
	return nil
}

// handleGetIO handles an HTTP request for the I/O summary. It returns a list of
// requested input streams and provided output streams.
func (s *server) handleGetIO() handlerFunc {
	return func(c *gin.Context) error {
		var response struct {
			RequestedInputStreams []string `json:"requested_input_streams"`
			ProvidedOutputStreams []string `json:"provided_output_streams"`
		}

		ioSummary, err := s.handler.ioSummary()
		if err != nil {
			return meh.Wrap(err, "get io summary", nil)
		}
		response.RequestedInputStreams = ioSummary.requestedInputStreams
		response.ProvidedOutputStreams = ioSummary.providedOutputStreams
		c.JSON(http.StatusOK, response)
		return nil
	}
}

func (s *server) handleProvideInputStream() handlerFunc {
	return func(c *gin.Context) error {
		streamName := c.Param("streamName")
		err := s.handler.readInputStream(streamName, c.Request.Body)
		if err != nil {
			return meh.Wrap(err, "read input stream", meh.Details{"stream_name": streamName})
		}
		c.Status(http.StatusOK)
		return nil
	}
}

func (s *server) handleReadInputStream() handlerFunc {
	return func(c *gin.Context) error {
		streamName := c.Param("streamName")
		err := s.handler.readInputStream(streamName, c.Request.Body)
		if err != nil {
			return meh.Wrap(err, "read input stream", meh.Details{"stream_name": streamName})
		}
		c.Status(http.StatusOK)
		return nil
	}
}

func (s *server) handleRequestOutputStream() handlerFunc {
	return func(c *gin.Context) error {
		streamName := c.Param("streamName")
		stream, err := s.handler.outputStreamByName(streamName)
		if err != nil {
			return meh.Wrap(err, "get output stream", meh.Details{"stream_name": streamName})
		}
		stream.stateCond.L.Lock()
		currentStreamState := stream.state
		currentWriterErr := stream.writerErr
		stream.stateCond.L.Unlock()
		// Respond, according to stream state.
		switch currentStreamState {
		case outputStreamStateWaitForOpen:
			// Wait until stream is not waiting for open anymore.
			stream.stateCond.L.Lock()
			for stream.state == outputStreamStateWaitForOpen {
				stream.stateCond.Wait()
			}
			stream.stateCond.L.Unlock()
			c.Status(http.StatusAccepted)
			return nil
		case outputStreamStateOpen:
			// Forward data.
			c.DataFromReader(http.StatusOK, -1, "application/binary", stream.readerForServer, nil)
			return nil
		case outputStreamStateDone:
			// Response that no more content is available.
			c.Status(http.StatusNoContent)
			return nil
		case outputStreamStateError:
			// Respond with error.
			return currentWriterErr
		default:
			return meh.NewInternalErr(fmt.Sprintf("unexpected output stream state: %v", currentStreamState),
				meh.Details{"stream_name": streamName})
		}
	}
}

func (s *server) handleShutdown() handlerFunc {
	return func(c *gin.Context) error {
		// First respond, then shutdown.
		c.Status(http.StatusOK)
		go func() {
			<-c.Request.Context().Done()
			// Shutdown the server properly to wait until the connection to LWEE is closed.
			_ = s.httpServer.Shutdown(context.Background())
			s.handler.shutdown()
		}()
		return nil
	}
}

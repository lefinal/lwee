package actionio

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"io"
	"sync"
	"time"
)

// DefaultReaderSourceBufferSize is the default source buffer size for Supplier.
const DefaultReaderSourceBufferSize = 10000000 // 10MB

type SourceReader struct {
	Name   string
	Open   <-chan struct{}
	Reader io.ReadCloser
}

type sourceReader struct {
	requester string
	open      chan<- struct{}
	writer    io.WriteCloser
}

type SourceWriter struct {
	Name   string
	Open   chan<- struct{}
	Writer io.WriteCloser
}

type sourceWriter struct {
	open   <-chan struct{}
	reader io.ReadCloser
}

type Supplier interface {
	RequestSource(sourceName string, requester string) SourceReader
	RegisterSourceProvider(sourceName string) (SourceWriter, error)
	Validate() error
	Forward(ctx context.Context) error
}

type SourceReadyNotifierFn func(sourceName string)

type Ingestor func(ctx context.Context, source io.Reader) error

type Outputter func(ctx context.Context, ready chan<- struct{}, writer io.WriteCloser) error

type sourceForwarder struct {
	logger      *zap.Logger
	copyOptions CopyOptions
	sourceName  string
	writer      sourceWriter
	readers     []sourceReader
}

func (forwarder *sourceForwarder) forward(ctx context.Context) error {
	defer func() {
		_ = forwarder.writer.reader.Close()
		for _, reader := range forwarder.readers {
			_ = reader.writer.Close()
		}
	}()
	// Wait for the writer being ready.
	start := time.Now()
	forwarder.logger.Debug("wait for source writer ready")
	select {
	case <-ctx.Done():
		return meh.NewInternalErrFromErr(ctx.Err(), "wait for source writer ready", nil)
	case <-forwarder.writer.open:
	}
	forwarder.logger.Debug(fmt.Sprintf("source writer has opened. forwarding to %d reader(s)", len(forwarder.readers)),
		zap.Time("source_writer_ready_at", time.Now()),
		zap.Duration("source_writer_ready_after", time.Since(start)))
	// Forward to all source readers.
	sourceReadersAsWriters := make([]io.Writer, 0)
	if len(forwarder.readers) > 0 {
		// Map to io.Writer for usage with io.MultiWriter.
		for _, r := range forwarder.readers {
			sourceReadersAsWriters = append(sourceReadersAsWriters, r.writer)
		}
		forwarder.logger.Debug("notify all readers of source being open")
		start = time.Now()
		err := notifyReadersOfOpenSource(ctx, forwarder.readers)
		if err != nil {
			return meh.Wrap(err, "notify readers of open source", nil)
		}
		forwarder.logger.Debug("all readers notified", zap.Duration("took", time.Since(start)))
	} else {
		// No readers registered. Discard.
		sourceReadersAsWriters = append(sourceReadersAsWriters, io.Discard)
		forwarder.logger.Debug("discard source output due to no readers registered")
	}
	copyDone := make(chan error)
	start = time.Now()
	var stats ioCopyStats
	go func() {
		forwarder.logger.Debug("copy data", zap.Int("source_readers", len(forwarder.readers)))
		var err error
		stats, err = ioCopyToMultiWithStats(forwarder.writer.reader, forwarder.copyOptions, sourceReadersAsWriters...)
		if err != nil {
			err = meh.Wrap(err, "copy", nil)
		}
		select {
		case <-ctx.Done():
		case copyDone <- err:
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-copyDone:
		if err != nil {
			return err
		}
	}
	writeTimesWithRequester := make(map[string]string)
	for i, writeTime := range stats.writeTimes {
		writeTimesWithRequester[forwarder.readers[i].requester] = writeTime.String()
	}
	forwarder.logger.Debug("source data copied",
		zap.Duration("took", time.Since(start)),
		zap.String("bytes_copied", logging.FormatByteCountDecimal(int64(stats.written))),
		zap.String("copy_buffer_size", logging.FormatByteCountDecimal(int64(stats.copyOptions.CopyBufferSize))),
		zap.Int("iop_count", stats.iopCount),
		zap.Duration("read_time", stats.readTime),
		zap.Duration("write_time_all", stats.writeTimeAll),
		zap.Duration("min_write_time", stats.minWriteTime),
		zap.Duration("max_write_time", stats.maxWriteTime),
		zap.Duration("avg_write_time", stats.avgWriteTime),
		zap.Duration("total_wait_for_next_p", stats.totalWaitForNextP),
		zap.Duration("total_distribute_p", stats.totalDistributeP),
		zap.Duration("total_wait_for_writes_after_distribute", stats.totalWaitForWritesAfterDistribute),
		zap.Any("write_times", writeTimesWithRequester),
		zap.Int("source_readers", len(forwarder.readers)))
	return nil
}

func notifyReadersOfOpenSource(ctx context.Context, readers []sourceReader) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, reader := range readers {
		reader := reader
		eg.Go(func() error {
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "notify reader of source being open", nil)
			case reader.open <- struct{}{}:
			}
			return nil
		})
	}
	return eg.Wait()
}

func NewSupplier(logger *zap.Logger, copyOptions CopyOptions) Supplier {
	return &supplier{
		logger:      logger,
		copyOptions: copyOptions,
	}
}

// supplier is the implementation of Supplier.
type supplier struct {
	logger      *zap.Logger
	copyOptions CopyOptions
	forwarders  []*sourceForwarder
	m           sync.Mutex
}

func (supplier *supplier) newForwarder(sourceName string) *sourceForwarder {
	return &sourceForwarder{
		logger:      supplier.logger.Named("source").Named(logging.WrapName(sourceName)),
		copyOptions: supplier.copyOptions,
		sourceName:  sourceName,
		writer:      sourceWriter{},
		readers:     make([]sourceReader, 0),
	}
}

func (supplier *supplier) RequestSource(sourceName string, requester string) SourceReader {
	supplier.logger.Debug(fmt.Sprintf("request source %q", sourceName))
	supplier.m.Lock()
	defer supplier.m.Unlock()
	open := make(chan struct{})
	reader, writer := io.Pipe()
	readerToKeepInternally := sourceReader{
		requester: requester,
		open:      open,
		writer:    writer,
	}
	readerToReturn := SourceReader{
		Name:   sourceName,
		Open:   open,
		Reader: reader,
	}
	// Add to correct forwarder.
	for _, forwarder := range supplier.forwarders {
		if forwarder.sourceName == sourceName {
			forwarder.readers = append(forwarder.readers, readerToKeepInternally)
			return readerToReturn
		}
	}
	// No forwarder found. Create one.
	forwarder := supplier.newForwarder(sourceName)
	forwarder.readers = append(forwarder.readers, readerToKeepInternally)
	supplier.forwarders = append(supplier.forwarders, forwarder)
	return readerToReturn
}

func (supplier *supplier) RegisterSourceProvider(sourceName string) (SourceWriter, error) {
	supplier.logger.Debug(fmt.Sprintf("provide source output %q", sourceName))
	supplier.m.Lock()
	defer supplier.m.Unlock()
	open := make(chan struct{})
	reader, writer := io.Pipe()
	writerToKeepInternally := sourceWriter{
		open:   open,
		reader: reader,
	}
	writerToReturn := SourceWriter{
		Name:   sourceName,
		Open:   open,
		Writer: writer,
	}
	// Check if forwarder for the same source already exists.
	for _, forwarder := range supplier.forwarders {
		if forwarder.sourceName != sourceName {
			continue
		}
		// Forwarder for the same source found.
		if forwarder.writer.reader != nil {
			return SourceWriter{}, meh.NewInternalErr(fmt.Sprintf("duplicate output for source %q", sourceName), nil)
		}
		forwarder.writer = writerToKeepInternally
		return writerToReturn, nil
	}
	// No forwarder found. Create one.
	forwarder := supplier.newForwarder(sourceName)
	forwarder.writer = writerToKeepInternally
	supplier.forwarders = append(supplier.forwarders, forwarder)
	return writerToReturn, nil
}

func (supplier *supplier) Validate() error {
	// Check for missing source providers (writers).
	for _, forwarder := range supplier.forwarders {
		if forwarder.writer.reader == nil {
			requestedByReaders := make([]string, 0)
			for _, reader := range forwarder.readers {
				requestedByReaders = append(requestedByReaders, reader.requester)
			}
			return meh.NewInternalErr(fmt.Sprintf("missing source provider for source %q", forwarder.sourceName),
				meh.Details{"requested_by_readers": requestedByReaders})
		}
	}
	return nil
}

func (supplier *supplier) Forward(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	supplier.m.Lock()
	for _, forwarder := range supplier.forwarders {
		forwarder := forwarder
		eg.Go(func() error {
			err := forwarder.forward(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("forward source %q", forwarder.sourceName), nil)
			}
			return nil
		})
	}
	supplier.m.Unlock()
	return eg.Wait()
}

package actionio

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/runinfo"
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
	Open   <-chan *AvailableOptimizations
	Reader io.ReadCloser
}

// WaitForOpen waits until the reader is open or the given context is done.
func (reader *SourceReader) WaitForOpen(ctx context.Context) (*AvailableOptimizations, error) {
	select {
	case <-ctx.Done():
		return nil, meh.NewInternalErrFromErr(ctx.Err(), "context done", meh.Details{"source_name": reader.Name})
	case availableOptimizations := <-reader.Open:
		return availableOptimizations, nil
	}
}

type sourceReader struct {
	sourceEntityName string
	// requesterName holds a string representation of the requesterName's identifier.
	// This is used for logging as well as cycle detection.
	requesterName string
	open          chan<- *AvailableOptimizations
	writer        io.WriteCloser
}

type SourceWriter struct {
	Name   string
	Open   chan<- AlternativeSourceAccess
	Writer io.WriteCloser
}

type sourceWriter struct {
	entityName string
	// providerName holds a string representation of the providerName's identifier.
	// This is used for logging as well as cycle detection.
	providerName string
	open         <-chan AlternativeSourceAccess
	reader       io.ReadCloser
}

type AlternativeSourceAccess struct {
	// Filename is a non-empty string if the source data is also available as file at
	// the specified location.
	Filename string
}

type AvailableOptimizations struct {
	// Filename is a non-empty string if only one reader requested the source and the
	// source data is available as file. The reader might then just move the file if
	// it provides file access, anyway.
	Filename string

	isSkipped        bool
	skipTransmission func()
}

// SkipTransmission reports to the source writer that the data has been
// transmitted manually and is not required to be sent anymore. Make sure to call
// SkipTransmission before closing the source reader.
func (optimizations *AvailableOptimizations) SkipTransmission() {
	optimizations.isSkipped = true
	if optimizations.skipTransmission != nil {
		optimizations.skipTransmission()
	}
}

// IsTransmissionSkipped returns true when SkipTransmission was called. If so, no
// data transmission will be performed. This is used when optimizations have been
// applied that copied all required data.
func (optimizations *AvailableOptimizations) IsTransmissionSkipped() bool {
	return optimizations.isSkipped
}

type Supplier interface {
	RequestSource(sourceName string, entityName string, requesterName string) SourceReader
	RegisterSourceProvider(sourceName string, entityName string, providerName string) (SourceWriter, error)
	Validate() error
	Forward(ctx context.Context) error
}

type SourceReadyNotifierFn func(sourceName string)

type Ingestor func(ctx context.Context, source io.Reader) error

type Outputter func(ctx context.Context, ready chan<- AlternativeSourceAccess, writer io.WriteCloser) error

type sourceForwarder struct {
	logger          *zap.Logger
	copyOptions     CopyOptions
	sourceName      string
	writer          sourceWriter
	readers         []sourceReader
	runInfoRecorder *runinfo.Recorder
}

func determineAvailableOptimizations(logger *zap.Logger, alternativeSourceAccess AlternativeSourceAccess, readers []sourceReader) AvailableOptimizations {
	availableOptimizations := AvailableOptimizations{}
	if alternativeSourceAccess.Filename != "" && len(readers) == 1 {
		logger.Debug("detected available optimization via filename")
		availableOptimizations.Filename = alternativeSourceAccess.Filename
	}
	return availableOptimizations
}

func (forwarder *sourceForwarder) forward(ctx context.Context) error {
	defer func() {
		_ = forwarder.writer.reader.Close()
		for _, reader := range forwarder.readers {
			_ = reader.writer.Close()
		}
	}()
	writeInfo := runinfo.IOWriteInfo{
		Requesters: forwarder.requesterNames(),
	}
	// Wait for the writer being ready.
	start := time.Now()
	writeInfo.WaitForOpenStart = start
	forwarder.logger.Debug("wait for source writer ready")
	var alternativeSourceAccess AlternativeSourceAccess
	select {
	case <-ctx.Done():
		return meh.NewInternalErrFromErr(ctx.Err(), "wait for source writer ready", nil)
	case alternativeSourceAccess = <-forwarder.writer.open:
	}
	writeInfo.WaitForOpenEnd = time.Now()
	forwarder.logger.Debug(fmt.Sprintf("source writer has opened. forwarding to %d reader(s)", len(forwarder.readers)),
		zap.Time("source_writer_ready_at", time.Now()),
		zap.Duration("source_writer_ready_after", time.Since(start)))
	// Forward to all source readers.
	sourceReadersAsWriters := make([]*ioCopyWriter, 0)

	if len(forwarder.readers) > 0 {
		availableOptimizations := determineAvailableOptimizations(forwarder.logger, alternativeSourceAccess, forwarder.readers)
		// Set up each reader and notify them about the source being open.
		forwarder.logger.Debug("notify all readers of source being open")
		readerNotificationsAboutOpenSource, notifyCtx := errgroup.WithContext(ctx)
		for _, reader := range forwarder.readers {
			reader := reader
			sourceReaderIOCopyWriter := newIOCopyWriter(reader.writer)
			sourceReadersAsWriters = append(sourceReadersAsWriters, sourceReaderIOCopyWriter)
			availableOptimizationsForReader := availableOptimizations
			availableOptimizationsForReader.skipTransmission = func() {
				sourceReaderIOCopyWriter.skip()
				forwarder.logger.Debug("source reader skipped transmission",
					zap.String("source_entity", reader.sourceEntityName),
					zap.String("reader_requester", reader.requesterName))
			}
			readerNotificationsAboutOpenSource.Go(func() error {
				select {
				case <-notifyCtx.Done():
					return meh.NewInternalErrFromErr(ctx.Err(), "notify reader of source being open", meh.Details{
						"source_entity":    reader.sourceEntityName,
						"reader_requester": reader.requesterName,
					})
				case reader.open <- &availableOptimizationsForReader:
				}
				return nil
			})
		}
		err := readerNotificationsAboutOpenSource.Wait()
		if err != nil {
			return meh.Wrap(err, "notify readers of source being open", nil)
		}
		start = time.Now()
		forwarder.logger.Debug("all readers notified", zap.Duration("took", time.Since(start)))
	} else {
		// No readers registered. Discard.
		sourceReadersAsWriters = append(sourceReadersAsWriters, newIOCopyWriter(io.Discard))
		forwarder.logger.Debug("discard source output due to no readers registered")
	}
	copyDone := make(chan error)
	start = time.Now()
	writeInfo.WriteStart = start
	var stats ioCopyStats
	go func() {
		forwarder.logger.Debug("copy data", zap.Int("source_readers", len(forwarder.readers)))
		copier := newIOMultiCopier(forwarder.writer.reader, forwarder.copyOptions, sourceReadersAsWriters)
		var err error
		stats, err = copier.copyToMultiWithStats()
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
	// We go by registered readers in order to avoid errors when no readers are
	// registered, but I/O discard is being passed to writing.
	if len(forwarder.readers) > 0 {
		for i := range stats.writeTimes {
			writeTimesWithRequester[forwarder.readers[i].requesterName] = stats.writeTimes[i].String()
		}
	}
	writeInfo.WriteEnd = time.Now()
	writeInfo.WrittenBytes = int64(stats.written)
	writeInfo.CopyBufferSizeBytes = int64(stats.copyOptions.CopyBufferSize)
	writeInfo.MinWriteTime = runinfo.Duration(stats.minWriteTime)
	writeInfo.MaxWriteTime = runinfo.Duration(stats.maxWriteTime)
	writeInfo.AvgWriteTime = runinfo.Duration(stats.avgWriteTime)
	writeInfo.TotalWaitForNextP = runinfo.Duration(stats.totalWaitForNextP)
	writeInfo.TotalDistributeP = runinfo.Duration(stats.totalDistributeP)
	writeInfo.TotalWaitForWritesAfterDistribute = runinfo.Duration(stats.totalWaitForWritesAfterDistribute)
	writeInfo.WriteTimes = writeTimesWithRequester
	forwarder.runInfoRecorder.RecordIOWriteInfo(forwarder.sourceName, writeInfo)
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

func (forwarder *sourceForwarder) requesterNames() []string {
	requestedByReaders := make([]string, 0)
	for _, reader := range forwarder.readers {
		requestedByReaders = append(requestedByReaders, reader.requesterName)
	}
	return requestedByReaders
}

// supplier is the implementation of Supplier.
type supplier struct {
	logger          *zap.Logger
	copyOptions     CopyOptions
	forwarders      []*sourceForwarder
	runInfoRecorder *runinfo.Recorder
	m               sync.Mutex
}

func NewSupplier(logger *zap.Logger, copyOptions CopyOptions, runInfoRecorder *runinfo.Recorder) Supplier {
	return &supplier{
		logger:          logger,
		copyOptions:     copyOptions,
		runInfoRecorder: runInfoRecorder,
	}
}

func (supplier *supplier) newForwarder(sourceName string) *sourceForwarder {
	return &sourceForwarder{
		logger:          supplier.logger.Named("source").Named(logging.WrapName(sourceName)),
		copyOptions:     supplier.copyOptions,
		sourceName:      sourceName,
		writer:          sourceWriter{},
		readers:         make([]sourceReader, 0),
		runInfoRecorder: supplier.runInfoRecorder,
	}
}

func (supplier *supplier) RequestSource(sourceName string, entityName string, requesterName string) SourceReader {
	supplier.logger.Debug(fmt.Sprintf("request source %q", sourceName))
	supplier.m.Lock()
	defer supplier.m.Unlock()
	open := make(chan *AvailableOptimizations)
	reader, writer := io.Pipe()
	readerToKeepInternally := sourceReader{
		sourceEntityName: entityName,
		requesterName:    requesterName,
		open:             open,
		writer:           writer,
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

func (supplier *supplier) RegisterSourceProvider(sourceName string, entityName string, providerName string) (SourceWriter, error) {
	supplier.logger.Debug(fmt.Sprintf("provide source output %q", sourceName))
	supplier.m.Lock()
	defer supplier.m.Unlock()
	open := make(chan AlternativeSourceAccess)
	reader, writer := io.Pipe()
	writerToKeepInternally := sourceWriter{
		entityName:   entityName,
		providerName: providerName,
		open:         open,
		reader:       reader,
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
			return SourceWriter{}, meh.NewInternalErr(fmt.Sprintf("duplicate output for source %q", sourceName), meh.Details{
				"provided_by":      forwarder.writer.providerName,
				"provided_also_by": providerName,
			})
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
			return meh.NewInternalErr(fmt.Sprintf("missing source provider for source %q", forwarder.sourceName),
				meh.Details{"requested_by_readers": forwarder.requesterNames()})
		}
	}
	// Check for cycles.
	err := assureNoCycles(supplier.forwarders)
	if err != nil {
		return meh.Wrap(err, "assure no cycles", nil)
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
				return meh.Wrap(err, fmt.Sprintf("forward source %q", forwarder.sourceName), meh.Details{
					"provider":   forwarder.writer.providerName,
					"requesters": forwarder.requesterNames(),
				})
			}
			return nil
		})
	}
	supplier.m.Unlock()
	return eg.Wait()
}

package actionio

import (
	"context"
	"errors"
	"github.com/lefinal/meh"
	"golang.org/x/sync/errgroup"
	"io"
	"math"
	"sync"
	"time"
)

type ioCopyWriter struct {
	dst       io.Writer
	written   int
	writeTime time.Duration

	skipped bool
	m       sync.Mutex
}

func newIOCopyWriter(dst io.Writer) *ioCopyWriter {
	return &ioCopyWriter{
		dst:       dst,
		written:   0,
		writeTime: 0,
	}
}

func (w *ioCopyWriter) skip() {
	w.m.Lock()
	defer w.m.Unlock()
	w.skipped = true
}

func (w *ioCopyWriter) shouldSkip() bool {
	w.m.Lock()
	defer w.m.Unlock()
	return w.skipped
}

func (w *ioCopyWriter) copyFrom(ctx context.Context, tasks chan []byte, notifyDone chan int) error {
	var start time.Time
	var p []byte
	var more bool
	var written int
	var err error
	var skipWrite bool
	for {
		// Wait for the next task.
		select {
		case <-ctx.Done():
			// Done.
			return nil
		case p, more = <-tasks:
			if !more {
				return nil
			}
		}
		// Write.
		if !skipWrite {
			start = time.Now()
			written, err = w.dst.Write(p)
			w.writeTime += time.Since(start)
			w.written += written
			if err != nil {
				skipWrite = w.shouldSkip()
				if !skipWrite {
					return meh.NewInternalErrFromErr(err, "write", nil)
				}
			}
		}
		// Notify that we have finished writing.
		if skipWrite {
			written = len(p)
		}
		select {
		case <-ctx.Done():
			return meh.NewInternalErrFromErr(ctx.Err(), "notify done", nil)
		case notifyDone <- written:
		}
	}
}

type ioCopyStats struct {
	copyOptions                       CopyOptions
	readTime                          time.Duration
	writeTimeAll                      time.Duration
	writeTimes                        []time.Duration
	minWriteTime                      time.Duration
	maxWriteTime                      time.Duration
	avgWriteTime                      time.Duration
	totalWaitForNextP                 time.Duration
	totalDistributeP                  time.Duration
	totalWaitForWritesAfterDistribute time.Duration
	iopCount                          int
	written                           int
}

func newBufferForReader(src io.Reader, bufferSize int) []byte {
	if limitedReader, ok := src.(*io.LimitedReader); ok && int64(bufferSize) > limitedReader.N {
		if limitedReader.N < 1 {
			bufferSize = 1
		} else {
			bufferSize = int(limitedReader.N)
		}
	}
	return make([]byte, bufferSize)
}

// CopyOptions represents options for copying data.
type CopyOptions struct {
	// CopyBufferSize is the buffer size for data.
	CopyBufferSize int
}

type ioMultiCopier struct {
	src     io.Reader
	options CopyOptions
	writers []*ioCopyWriter
	stats   ioCopyStats
}

func newIOMultiCopier(src io.Reader, options CopyOptions, writers []*ioCopyWriter) *ioMultiCopier {
	const minBufferSize = 32 * 1024 // 32kB
	if options.CopyBufferSize < minBufferSize {
		options.CopyBufferSize = minBufferSize
	}
	stats := ioCopyStats{
		copyOptions:  options,
		writeTimes:   make([]time.Duration, len(writers)),
		minWriteTime: math.MaxInt,
		maxWriteTime: -1,
		avgWriteTime: -1,
	}
	return &ioMultiCopier{
		src:     src,
		options: options,
		writers: writers,
		stats:   stats,
	}
}

func (copier *ioMultiCopier) copyToMultiWithStats() (ioCopyStats, error) {
	// Launch goroutines for writers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	writeDone := make(chan int)
	writeTasks := make(chan []byte)

	// Start writers.
	for _, dst := range copier.writers {
		dst := dst
		eg.Go(func() error {
			return dst.copyFrom(ctx, writeTasks, writeDone)
		})
	}

	// Launch goroutine for reader.
	pReadyToWrite := make(chan []byte)
	eg.Go(func() error {
		err := copier.reader(ctx, pReadyToWrite)
		if err != nil {
			return meh.Wrap(err, "copier reader", nil)
		}
		return nil
	})

	// Launch goroutine for forwarder.
	eg.Go(func() error {
		err := copier.forwarder(ctx, pReadyToWrite, writeTasks, writeDone)
		if err != nil {
			return meh.Wrap(err, "copier forwarder", nil)
		}
		return nil
	})

	err := eg.Wait()
	// Finish up stats.
	for i, writer := range copier.writers {
		copier.stats.writeTimes[i] = writer.writeTime
		copier.stats.minWriteTime = min(copier.stats.minWriteTime, writer.writeTime)
		copier.stats.maxWriteTime = max(copier.stats.maxWriteTime, writer.writeTime)
		copier.stats.avgWriteTime += writer.writeTime
	}
	copier.stats.avgWriteTime /= time.Duration(len(copier.writers))
	return copier.stats, err
}

func (copier *ioMultiCopier) reader(ctx context.Context, pReadyToWrite chan []byte) error {
	const maxCopyBufferTime = 200 * time.Millisecond
	defer close(pReadyToWrite)
	minBufferSizeForWrite := copier.options.CopyBufferSize / 2
	// Create two buffers.
	buffers := make([][]byte, 2)
	for i := range buffers {
		buffers[i] = newBufferForReader(copier.src, copier.options.CopyBufferSize)
	}
	// Read in turns.
	currentBufferNum := 0
	var buf []byte
	var total, n int
	var pStart, start time.Time
	var err error
	for {
		// Read the next segment.
		pStart = time.Now()
		total = 0
		for {
			start = time.Now()
			n, err = copier.src.Read(buf[total:])
			copier.stats.readTime += time.Since(start)
			total += n
			if err != nil {
				break
			}
			if total >= minBufferSizeForWrite || total >= len(buf)-1 || time.Since(pStart) >= maxCopyBufferTime {
				break
			}
		}
		// Notify about next p ready to write.
		if total > 0 {
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "wait for pickup of next p", nil)
			case pReadyToWrite <- buf[0:total]:
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return meh.NewInternalErrFromErr(err, "read", nil)
		}

		// Next buffer.
		currentBufferNum++
		if currentBufferNum >= len(buffers) {
			currentBufferNum = 0
		}
		buf = buffers[currentBufferNum]
	}
}

func (copier *ioMultiCopier) forwarder(ctx context.Context, pReadyToWrite chan []byte, writeTasks chan []byte, writeDone chan int) error {
	defer close(writeTasks)
	var p []byte
	var more bool
	var start time.Time
	for {
		// Wait for the next p to write.
		start = time.Now()
		select {
		case <-ctx.Done():
			return meh.NewInternalErrFromErr(ctx.Err(), "wait for next p to write", nil)
		case p, more = <-pReadyToWrite:
			if !more {
				return nil
			}
		}
		copier.stats.iopCount++
		copier.stats.totalWaitForNextP += time.Since(start)
		// Write to outputs.
		start = time.Now()
		for i := 0; i < len(copier.writers); i++ {
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "wait for all writers to pick up next slice", nil)
			case writeTasks <- p:
			}
		}
		copier.stats.totalDistributeP += time.Since(start)
		// Wait for all writers to have finished.
		start = time.Now()
		for i := 0; i < len(copier.writers); i++ {
			select {
			case <-ctx.Done():
				return meh.NewInternalErrFromErr(ctx.Err(), "wait for all writers to finish writing", nil)
			case writtenToWriter := <-writeDone:
				if writtenToWriter < 0 || len(p) < writtenToWriter {
					return errors.New("invalid write result")
				}
				if writtenToWriter != len(p) {
					return io.ErrShortWrite
				}
			}
		}
		copier.stats.totalWaitForWritesAfterDistribute += time.Since(start)
		// All writes ok.
		copier.stats.writeTimeAll += time.Since(start)
		copier.stats.written += len(p)
	}
}

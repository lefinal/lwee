package actionio

import (
	"context"
	"errors"
	"github.com/lefinal/meh"
	"golang.org/x/sync/errgroup"
	"io"
	"math"
	"time"
)

type ioCopyWriter struct {
	dst       io.Writer
	written   int
	writeTime time.Duration
}

func (w *ioCopyWriter) copyFrom(ctx context.Context, tasks chan []byte, notifyDone chan int) error {
	var start time.Time
	var p []byte
	var more bool
	var written int
	var err error
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
		start = time.Now()
		written, err = w.dst.Write(p)
		w.writeTime += time.Since(start)
		w.written += written
		if err != nil {
			return meh.NewInternalErrFromErr(err, "write", nil)
		}
		// Notify that we have finished writing.
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

type CopyOptions struct {
	CopyBufferSize int
}

func ioCopyToMultiWithStats(src io.Reader, options CopyOptions, writers ...io.Writer) (ioCopyStats, error) {
	const minBufferSize = 32 * 1024 // 32kB
	const maxCopyBufferTime = 200 * time.Millisecond
	if options.CopyBufferSize < minBufferSize {
		options.CopyBufferSize = minBufferSize
	}

	stats := ioCopyStats{
		copyOptions:  options,
		writeTimes:   make([]time.Duration, 0),
		minWriteTime: math.MaxInt,
		maxWriteTime: -1,
		avgWriteTime: -1,
	}

	// Launch goroutines for writers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	writeDone := make(chan int)
	writeTasks := make(chan []byte)
	copyWriters := make([]*ioCopyWriter, 0, len(writers))
	for _, writer := range writers {
		copyWriters = append(copyWriters, &ioCopyWriter{
			dst: writer,
		})
	}
	for _, dst := range copyWriters {
		dst := dst
		eg.Go(func() error {
			return dst.copyFrom(ctx, writeTasks, writeDone)
		})
	}

	// Launch goroutine for reader.
	pReadyToWrite := make(chan []byte)
	eg.Go(func() error {
		defer close(pReadyToWrite)
		minBufferSizeForWrite := options.CopyBufferSize / 2
		// Create two buffers.
		buffers := make([][]byte, 2)
		for i := range buffers {
			buffers[i] = newBufferForReader(src, options.CopyBufferSize)
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
				n, err = src.Read(buf[total:])
				stats.readTime += time.Since(start)
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
	})

	// Launch goroutine for forwarder.
	eg.Go(func() error {
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
			stats.iopCount++
			stats.totalWaitForNextP += time.Since(start)
			// Write to outputs.
			start = time.Now()
			for i := 0; i < len(writers); i++ {
				select {
				case <-ctx.Done():
					return meh.NewInternalErrFromErr(ctx.Err(), "wait for all writers to pick up next slice", nil)
				case writeTasks <- p:
				}
			}
			stats.totalDistributeP += time.Since(start)
			// Wait for all writers to have finished.
			start = time.Now()
			for i := 0; i < len(writers); i++ {
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
			stats.totalWaitForWritesAfterDistribute += time.Since(start)
			// All writes ok.
			stats.writeTimeAll += time.Since(start)
			stats.written += len(p)
		}
	})

	err := eg.Wait()
	// Finish up stats.
	for _, writer := range copyWriters {
		stats.writeTimes = append(stats.writeTimes, writer.writeTime)
		stats.minWriteTime = min(stats.minWriteTime, writer.writeTime)
		stats.maxWriteTime = max(stats.maxWriteTime, writer.writeTime)
		stats.avgWriteTime += writer.writeTime
	}
	stats.avgWriteTime /= time.Duration(len(copyWriters))
	return stats, err
}

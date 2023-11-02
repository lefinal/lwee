package testutil

import (
	"context"
	"pgregory.net/rapid"
	"testing"
	"time"
)

// TestFailer is an abstraction for failing tests instantly.
type TestFailer interface {
	FailNow(string, ...interface{}) bool
}

// tfailer implements TestFailer for testing.T.
type tfailer struct {
	t *testing.T
}

func (tf *tfailer) FailNow(s string, i ...interface{}) bool {
	tf.t.Fatalf(s, i...)
	return true
}

// rapidTFailer implements TestFailer for rapid.T.
type rapidTFailer struct {
	t *rapid.T
}

func (tf rapidTFailer) FailNow(s string, i ...interface{}) bool {
	tf.t.Fatalf(s, i...)
	return true
}

// TestFailerFromT creates a TestFailer from the given testing.T.
func TestFailerFromT(t *testing.T) TestFailer {
	return &tfailer{t: t}
}

// TestFailerFromRapidT creates a TestFailer from the given rapid.T.
func TestFailerFromRapidT(t *rapid.T) TestFailer {
	return rapidTFailer{t: t}
}

// NewTimeout wraps timeout-detection-functionality. It creates a
// timeout-context, a cancel-function and a wait function. The intended usage is
// creating everything using NewTimeout and then either deferring or, if post
// checks are required, calling the wait-function. The wait-function will also
// definitely call the cancel-function.
func NewTimeout(failer TestFailer, timeout time.Duration) (context.Context, context.CancelFunc, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return ctx, cancel, func() {
		defer cancel()
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			failer.FailNow("timeout", "should not time out")
		}
	}
}

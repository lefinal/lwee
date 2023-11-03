// Package testutil provides different utils for testing.
package testutil

import (
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/mock"
	"math/rand"
	"reflect"
	"time"
)

// NewUUIDV4 returns a new v4 uuid.UUID. If generation fails, it panicks.
func NewUUIDV4() uuid.UUID {
	id, err := uuid.NewV4()
	if err != nil {
		panic(err)
	}
	return id
}

// NewRandomTime creates a new time.Time with random value.
func NewRandomTime() time.Time {
	t := time.UnixMicro(rand.Int63())
	t = t.In(time.UTC)
	// Limit year because of JSON marshalling.
	if t.Year() >= 9999 {
		t = t.AddDate(-t.Year()+9900, 0, 0)
	}
	return t
}

// UnsetCallByMethod is a temporary replacement for Unset in mock.Call. This is
// needed because of a bug in testify that causes a panic. The bug is fixed by
// https://github.com/stretchr/testify/pull/1250 but the fix not released, yet.
func UnsetCallByMethod(m *mock.Mock, methodName string) {
	newExpectedCalls := make([]*mock.Call, 0, len(m.ExpectedCalls))
	for _, expectedCall := range m.ExpectedCalls {
		if expectedCall.Method != methodName {
			newExpectedCalls = append(newExpectedCalls, expectedCall)
		}
	}
	if len(m.ExpectedCalls) == len(newExpectedCalls) {
		panic(fmt.Sprintf("\n\nmock: Could not find expected call with method name\n-----------------------------\n\n%s\n\n", methodName))
	}
	m.ExpectedCalls = newExpectedCalls
}

// unsetClone is a primitive copy of
// https://github.com/stretchr/testify/blob/master/mock/mock.go until
// https://github.com/stretchr/testify/pull/1250 is released.
func unsetClone(m *mock.Mock, methodName string, args ...any) {
	c := m.On(methodName, args...)

	for _, arg := range c.Arguments {
		if v := reflect.ValueOf(arg); v.Kind() == reflect.Func {
			panic(fmt.Sprintf("cannot use Func in expectations. Use mock.AnythingOfType(\"%T\")", arg))
		}
	}

	foundMatchingCall := false

	// in-place filter slice for calls to be removed - iterate from 0'th to last skipping unnecessary ones
	var index int // write index
	for _, call := range c.Parent.ExpectedCalls {
		if call.Method == c.Method {
			_, diffCount := call.Arguments.Diff(c.Arguments)
			if diffCount == 0 {
				foundMatchingCall = true
				// Remove from ExpectedCalls - just skip it
				continue
			}
		}
		c.Parent.ExpectedCalls[index] = call
		index++
	}
	// trim slice up to last copied index
	c.Parent.ExpectedCalls = c.Parent.ExpectedCalls[:index]

	if !foundMatchingCall {
		panic(fmt.Sprintf(`mock: Could not find expected call to clean
----------------------
Method: %s
Arguments: %s
`, c.Method, c.Arguments))
	}
}

// UnsetAndOn unsets the given expected call and expects a new one which is
// returned for further usage like return, etc.
func UnsetAndOn(m *mock.Mock, methodName string, args ...any) *mock.Call {
	unsetClone(m, methodName, args...)
	return m.On(methodName, args...)
}

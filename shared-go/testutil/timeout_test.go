package testutil

import (
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type TestFailerMock struct {
	mock.Mock
}

func (m *TestFailerMock) FailNow(s string, i ...interface{}) bool {
	return m.Called(s, i).Bool(0)
}

// NewTimeoutSuite tests NewTimeout.
type NewTimeoutSuite struct {
	suite.Suite
	failer  *TestFailerMock
	timeout time.Duration
}

func (suite *NewTimeoutSuite) SetupTest() {
	suite.failer = &TestFailerMock{}
	suite.timeout = 100 * time.Millisecond
}

func (suite *NewTimeoutSuite) TestTimeout() {
	suite.failer.On("FailNow", mock.Anything, mock.Anything).Return(true)
	defer suite.failer.AssertExpectations(suite.T())
	timeout, cancel, wait := NewTimeout(suite.failer, suite.timeout)

	<-time.After(suite.timeout * 2)
	cancel()

	wait()
	<-timeout.Done()
}

func (suite *NewTimeoutSuite) TestOK() {
	timeout, cancel, wait := NewTimeout(suite.failer, suite.timeout)

	<-time.After(suite.timeout / 2)
	cancel()

	wait()
	<-timeout.Done()
	suite.failer.AssertNotCalled(suite.T(), "FailNow", mock.Anything, mock.Anything)
}

func TestNewTimeout(t *testing.T) {
	suite.Run(t, new(NewTimeoutSuite))
}

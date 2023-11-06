package commandassert

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"runtime"
	"testing"
)

func TestShouldContain(t *testing.T) {
	tests := []struct {
		actual        string
		target        string
		wantAssertErr bool
	}{
		{
			actual: "Hello World!",
			target: "World!",
		},
		{
			actual: "Hello World!",
			target: "Hello World!",
		},
		{
			actual: "\nHello\n\n\n",
			target: "Hello",
		},
		{
			actual: "My License - 1.2.3 - Hello!",
			target: "1.2.3",
		},
		{
			actual:        "Hello World!",
			target:        "Goodbye!",
			wantAssertErr: true,
		},
		{
			actual: "Hello World!",
			target: "",
		},
		{
			actual: "Hello World!",
			target: " ",
		},
		{
			actual: "Hello World!\n#",
			target: "#",
		},
	}

	for testNum, tt := range tests {
		t.Run(fmt.Sprintf("test#%d", testNum), func(t *testing.T) {
			assertion, err := New(Options{
				Run:    []string{"echo", tt.actual},
				Should: ShouldContain,
				Target: tt.target,
			})
			require.NoError(t, err, "create should not fail")
			err = assertion.Assert(context.Background())
			if tt.wantAssertErr {
				assert.Error(t, err, "assert should fail")
			} else {
				assert.NoError(t, err, "assert should not fail")
			}
		})
	}
}

func TestShouldEqual(t *testing.T) {
	tests := []struct {
		actual        string
		target        string
		wantAssertErr bool
	}{
		{
			actual: "Hello World!",
			target: "Hello World!",
		},
		{
			actual:        "Hello World!",
			target:        "Hello my World!",
			wantAssertErr: true,
		},
		{
			actual: "\nHello\n\n\n",
			target: "Hello",
		},
		{
			actual:        "My License - 1.2.3 - Hello!",
			target:        "1.2.3",
			wantAssertErr: true,
		},
		{
			actual:        "Hello World!",
			target:        "Goodbye!",
			wantAssertErr: true,
		},
		{
			actual:        "Hello World!",
			target:        "",
			wantAssertErr: true,
		},
		{
			actual: "",
			target: "",
		},
		{
			actual:        "Hello World!\n#",
			target:        "#",
			wantAssertErr: true,
		},
	}

	for testNum, tt := range tests {
		t.Run(fmt.Sprintf("test#%d", testNum), func(t *testing.T) {
			assertion, err := New(Options{
				Run:    []string{"echo", tt.actual},
				Should: ShouldEqual,
				Target: tt.target,
			})
			require.NoError(t, err, "create should not fail")
			err = assertion.Assert(context.Background())
			if tt.wantAssertErr {
				assert.Error(t, err, "assert should fail")
			} else {
				assert.NoError(t, err, "assert should not fail")
			}
		})
	}
}

func TestShouldMatchRegex(t *testing.T) {
	tests := []struct {
		actual        string
		target        string
		wantCreateErr bool
		wantAssertErr bool
	}{
		{
			actual:        "Hello World!",
			target:        "",
			wantCreateErr: true,
		},
		{
			actual:        "Hello World!",
			target:        "\\[,,*,]++",
			wantCreateErr: true,
		},
		{
			actual: "Hello World!",
			target: "World!",
		},
		{
			actual: "Hello World!",
			target: "Hello World!",
		},
		{
			actual: "\nHello\n\n\n",
			target: "Hello",
		},
		{
			actual: "My License - 1.2.3 - Hello!",
			target: "1.*.*",
		},
		{
			actual:        "Hello World!",
			target:        "Goodbye!",
			wantAssertErr: true,
		},
		{
			actual: "Hello World!",
			target: "^Hello World!$",
		},
		{
			actual:        "Hello World!",
			target:        "^Hello$",
			wantAssertErr: true,
		},
		{
			actual: "Hello World!\n#",
			target: "#",
		},
	}

	for testNum, tt := range tests {
		t.Run(fmt.Sprintf("test#%d", testNum), func(t *testing.T) {
			assertion, err := New(Options{
				Run:    []string{"echo", tt.actual},
				Should: ShouldMatchRegex,
				Target: tt.target,
			})
			if tt.wantCreateErr {
				assert.Error(t, err, "create should fail")
				return
			} else {
				require.NoError(t, err, "create should not fail")
			}
			err = assertion.Assert(context.Background())
			if tt.wantAssertErr {
				assert.Error(t, err, "assert should fail")
			} else {
				assert.NoError(t, err, "assert should not fail")
			}
		})
	}
}

type AssertionSuite struct {
	suite.Suite
	options Options
}

func (suite *AssertionSuite) SetupTest() {
	if runtime.GOOS == "windows" {
		suite.T().Skip()
		return
	}
	suite.options = Options{
		Run:    []string{"echo", "Hello World!"},
		Should: ShouldContain,
		Target: "Hello",
	}
}

func (suite *AssertionSuite) TestNoCommands() {
	suite.options.Run = nil
	_, err := New(suite.options)
	suite.Error(err, "create should fail")
}

func (suite *AssertionSuite) TestEmptyCommand() {
	suite.options.Run = []string{""}
	_, err := New(suite.options)
	suite.Error(err, "create should fail")
}

func (suite *AssertionSuite) TestCommandNotFound() {
	suite.options.Run = []string{"/bin/i/do/not/exist"}
	_, err := New(suite.options)
	suite.Error(err, "create should fail")
}

func (suite *AssertionSuite) TestRunCommandFail() {
	suite.options.Run = []string{"cp", "--unknown-flag"}
	assertion, err := New(suite.options)
	suite.Require().NoError(err, "create should not fail")
	err = assertion.Assert(context.Background())
	suite.Error(err, "assert should fail")
}

func (suite *AssertionSuite) TestOK() {
	assertion, err := New(suite.options)
	suite.Require().NoError(err, "create should not fail")
	err = assertion.Assert(context.Background())
	suite.NoError(err, "assert should not fail")
}

func TestAssertion(t *testing.T) {
	suite.Run(t, new(AssertionSuite))
}

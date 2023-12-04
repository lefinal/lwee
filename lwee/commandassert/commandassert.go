// Package commandassert provides assertion functionality for running commands.
package commandassert

import (
	"context"
	"fmt"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os/exec"
	"regexp"
	"strings"
)

// ShouldType is the type of assertion to make.
type ShouldType string

const (
	// ShouldContain checks whether the target is contained in the output.
	ShouldContain ShouldType = "contain"
	// ShouldEqual checks whether the target is equal to the output.
	ShouldEqual ShouldType = "equal"
	// ShouldMatchRegex checks whether the output matches the target regular
	// expression.
	ShouldMatchRegex ShouldType = "match-regex"
)

// Options for Assertion.
type Options struct {
	Logger *zap.Logger
	// Run is the command to run.
	Run []string
	// Should is the type of check to perform.
	Should ShouldType
	// Target is the value to use for the assertion. This may be a string the output
	// is expected to be equal to, a regular expression, etc. This depends on Should.
	Target string
}

// Assertion allows performing an assertion via Assert. This will run the
// specified command and perform assertions on it. If running the command or any
// assertion fails, an error is returned.
type Assertion interface {
	Assert(ctx context.Context) error
}

// New creates a new Assertion with the given Options. Run it with
// Assertion.Assert.
func New(options Options) (Assertion, error) {
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	options.Logger = options.Logger.With(zap.Any("should_type", options.Should))
	if len(options.Run) == 0 || options.Run[0] == "" {
		return nil, meh.NewBadInputErr("missing run command", nil)
	}
	_, err := exec.LookPath(options.Run[0])
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "look up command path", meh.Details{"command": options.Run[0]})
	}
	assertion := &assertion{
		logger:  options.Logger,
		options: options,
	}
	assertion.check, err = newAssertionCheckFunc(options)
	if err != nil {
		return nil, meh.Wrap(err, "new assertion check func", meh.Details{"want_type": options.Should})
	}
	return assertion, nil
}

type assertionCheckFunc func(ctx context.Context, val string) error

func newAssertionCheckFunc(options Options) (assertionCheckFunc, error) {
	var checkFunc assertionCheckFunc
	var err error
	switch options.Should {
	case ShouldContain:
		checkFunc, err = newShouldContainAssertionCheckFunc(options)
		if err != nil {
			return nil, meh.Wrap(err, "new should-contain assertion check func", nil)
		}
	case ShouldEqual:
		checkFunc, err = newShouldEqualAssertionCheckFunc(options)
		if err != nil {
			return nil, meh.Wrap(err, "new should-equal assertion check func", nil)
		}
	case ShouldMatchRegex:
		checkFunc, err = newShouldMatchRegexAssertionCheckFunc(options)
		if err != nil {
			return nil, meh.Wrap(err, "new should-match-regex assertion check func", nil)
		}
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported should-type: %v", options.Should), nil)
	}
	return checkFunc, nil
}

// newShouldContainAssertionCheckFunc creates an assertionCheckFunc for
// ShouldContain.
func newShouldContainAssertionCheckFunc(options Options) (assertionCheckFunc, error) {
	return func(_ context.Context, val string) error {
		if !strings.Contains(val, options.Target) {
			return meh.NewBadInputErr(fmt.Sprintf("value %q does not contain target %q", val, options.Target), meh.Details{
				"should_contain": options.Target,
			})
		}
		return nil
	}, nil
}

// newShouldEqualAssertionCheckFunc creates an assertionCheckFunc for
// ShouldEqual.
func newShouldEqualAssertionCheckFunc(options Options) (assertionCheckFunc, error) {
	return func(_ context.Context, val string) error {
		val = normalizeValue(val)
		if val != options.Target {
			return meh.NewBadInputErr(fmt.Sprintf("value %q does not equal target %q", val, options.Target), meh.Details{
				"expected": options.Target,
				"actual":   val,
			})
		}
		return nil
	}, nil
}

// newShouldMatchRegexAssertionCheckFunc creates an assertionCheckFunc for
// ShouldMatchRegex.
func newShouldMatchRegexAssertionCheckFunc(options Options) (assertionCheckFunc, error) {
	if options.Target == "" {
		return nil, meh.NewBadInputErr("missing target regex", nil)
	}
	expr, err := regexp.Compile(options.Target)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "compile regex", meh.Details{"was": options.Target})
	}
	return func(_ context.Context, val string) error {
		val = normalizeValue(val)
		if !expr.MatchString(val) {
			return meh.NewBadInputErr(fmt.Sprintf("value %q does not match regex %q", val, expr.String()), nil)
		}
		return nil
	}, nil
}

func normalizeValue(val string) string {
	return strings.Trim(val, "\n")
}

type assertion struct {
	logger  *zap.Logger
	options Options
	check   assertionCheckFunc
}

func (assertion *assertion) Assert(ctx context.Context) error {
	// Run command.
	cmd := exec.CommandContext(ctx, assertion.options.Run[0], assertion.options.Run[1:]...)
	runOutput, err := cmd.CombinedOutput()
	if err != nil {
		return meh.NewBadInputErrFromErr(err, "run command", meh.Details{
			"run_cmd": assertion.options.Run,
		})
	}
	assertion.logger.Debug(fmt.Sprintf("got run output:\n>>>\n%s\n<<<", string(runOutput)),
		zap.Strings("command", assertion.options.Run))
	err = assertion.check(ctx, string(runOutput))
	if err != nil {
		return meh.Wrap(err, "check", meh.Details{
			"run_output": runOutput,
		})
	}
	return nil
}

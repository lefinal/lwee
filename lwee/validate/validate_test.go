package validate

import (
	"github.com/lefinal/nulls"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"testing"
)

func testValidator[T any](value T, assertion Assertion[T], moreAssertions ...Assertion[T]) func(reporter *Reporter) {
	return func(reporter *Reporter) {
		ForField(reporter, field.NewPath(""), value, assertion, moreAssertions...)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expectErr bool
		validate  func(reporter *Reporter)
	}{
		// Assert not empty.
		{
			name:      "assert not empty int is set",
			expectErr: false,
			validate:  testValidator[int](42, AssertNotEmpty[int]()),
		},
		{
			name:      "assert not empty int not set",
			expectErr: true,
			validate:  testValidator[int](0, AssertNotEmpty[int]()),
		},
		{
			name:      "assert not empty uint8 is set",
			expectErr: false,
			validate:  testValidator[uint8](42, AssertNotEmpty[uint8]()),
		},
		{
			name:      "assert not empty uint8 not set",
			expectErr: true,
			validate:  testValidator[uint8](0, AssertNotEmpty[uint8]()),
		},
		{
			name:      "assert not empty string is set",
			expectErr: false,
			validate:  testValidator[string]("Hello World!", AssertNotEmpty[string]()),
		},
		{
			name:      "assert not empty string not set",
			expectErr: true,
			validate:  testValidator[string]("", AssertNotEmpty[string]()),
		},

		// Assert duration.
		{
			name:      "assert duration ok",
			expectErr: false,
			validate: testValidator[string]("10s",
				AssertDuration()),
		},
		{
			name:      "assert duration empty",
			expectErr: true,
			validate: testValidator[string]("",
				AssertDuration()),
		},
		{
			name:      "assert duration invalid",
			expectErr: true,
			validate: testValidator[string]("Hello World!",
				AssertDuration()),
		},

		// Assert if optional string set.
		{
			name:      "assert if optional string set no assertions",
			expectErr: true,
			validate: testValidator[nulls.String](nulls.String{},
				AssertIfOptionalStringSet()),
		},
		{
			name:      "assert if optional string set not set",
			expectErr: false,
			validate: testValidator[nulls.String](nulls.String{},
				AssertIfOptionalStringSet(
					AssertDuration(),
				)),
		},
		{
			name:      "assert if optional string set is set invalid",
			expectErr: true,
			validate: testValidator[nulls.String](nulls.NewString("Hello World!"),
				AssertIfOptionalStringSet(
					AssertDuration(),
				)),
		},
		{
			name:      "assert if optional string set is set ok",
			expectErr: false,
			validate: testValidator[nulls.String](nulls.NewString("100ms"),
				AssertIfOptionalStringSet(
					AssertDuration(),
				)),
		},

		// Assert if optional int set.
		{
			name:      "assert if optional int set no assertions",
			expectErr: true,
			validate: testValidator[nulls.Int](nulls.Int{},
				AssertIfOptionalIntSet()),
		},
		{
			name:      "assert if optional int set not set",
			expectErr: false,
			validate: testValidator[nulls.Int](nulls.Int{},
				AssertIfOptionalIntSet(
					AssertGreater(0),
					AssertGreater(42),
				)),
		},
		{
			name:      "assert if optional int set is set invalid",
			expectErr: true,
			validate: testValidator[nulls.Int](nulls.NewInt(5),
				AssertIfOptionalIntSet(
					AssertGreater(0),
					AssertGreater(42),
				)),
		},
		{
			name:      "assert if optional int set is set ok",
			expectErr: false,
			validate: testValidator[nulls.Int](nulls.NewInt(100),
				AssertIfOptionalIntSet(
					AssertGreater(0),
					AssertGreater(42),
				)),
		},

		// Assert greater.
		{
			name:      "assert greater but equal",
			expectErr: true,
			validate: testValidator(123,
				AssertGreater(123),
			),
		},
		{
			name:      "assert greater but less",
			expectErr: true,
			validate: testValidator(30,
				AssertGreater(123),
			),
		},
		{
			name:      "assert greater ok",
			expectErr: false,
			validate: testValidator(140,
				AssertGreater(123),
			),
		},

		// Assert greater equals.
		{
			name:      "assert greater equals and is equal",
			expectErr: false,
			validate: testValidator(123,
				AssertGreaterEq(123),
			),
		},
		{
			name:      "assert greater equals but less",
			expectErr: true,
			validate: testValidator(30,
				AssertGreaterEq(123),
			),
		},
		{
			name:      "assert greater equals ok",
			expectErr: false,
			validate: testValidator(140,
				AssertGreaterEq(123),
			),
		},

		// Assert less.
		{
			name:      "assert less but equal",
			expectErr: true,
			validate: testValidator(123,
				AssertLess(123),
			),
		},
		{
			name:      "assert less but greater",
			expectErr: true,
			validate: testValidator(500,
				AssertLess(123),
			),
		},
		{
			name:      "assert less ok",
			expectErr: false,
			validate: testValidator(15,
				AssertLess(123),
			),
		},

		// Assert less equals.
		{
			name:      "assert less equals and is equal",
			expectErr: false,
			validate: testValidator(123,
				AssertLessEq(123),
			),
		},
		{
			name:      "assert less equals but greater",
			expectErr: true,
			validate: testValidator(500,
				AssertLessEq(123),
			),
		},
		{
			name:      "assert less equals ok",
			expectErr: false,
			validate: testValidator(43,
				AssertLessEq(123),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reporter := NewReporter()
			tt.validate(reporter)
			errList := reporter.Report().Errors
			if tt.expectErr {
				assert.NotEmpty(t, errList)
			} else {
				assert.Empty(t, errList)
			}
		})
	}
}

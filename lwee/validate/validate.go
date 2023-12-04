// Package validate is for entity validation. It provides a Report that contains
// warnings and errors during validation as well as helper methods in the form of
// Assertion.
package validate

import (
	"cmp"
	"fmt"
	"github.com/lefinal/nulls"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"time"
)

// Path represents the path from some root to a field.
type Path = field.Path

// Assertion returns a non-empty error message if the given value does not
// satisfy the requirements.
type Assertion[T any] func(val T) string

// AssertNotEmpty is an Assertion for the value not being equal to its empty value.
func AssertNotEmpty[T comparable]() Assertion[T] {
	return func(val T) string {
		var empty T
		if val == empty {
			return "required"
		}
		return ""
	}
}

// AssertDuration is an Assertion for the value being a valid duration string.
func AssertDuration[T string]() Assertion[T] {
	return func(val T) string {
		_, err := time.ParseDuration(string(val))
		if err != nil {
			return err.Error()
		}
		return ""
	}
}

// AssertIfOptionalStringSet checks the given Assertion-list if the value is set.
func AssertIfOptionalStringSet(assertions ...Assertion[string]) Assertion[nulls.String] {
	return func(val nulls.String) string {
		if len(assertions) == 0 {
			return "internal error: no assertions"
		}
		if !val.Valid {
			return ""
		}
		for _, assertion := range assertions {
			errMessage := assertion(val.String)
			if errMessage != "" {
				return errMessage
			}
		}
		return ""
	}
}

// AssertIfOptionalIntSet checks the given Assertion-list if the value is set.
func AssertIfOptionalIntSet(assertions ...Assertion[int]) Assertion[nulls.Int] {
	return func(val nulls.Int) string {
		if len(assertions) == 0 {
			return "internal error: no assertions"
		}
		if !val.Valid {
			return ""
		}
		for _, assertion := range assertions {
			errMessage := assertion(val.Int)
			if errMessage != "" {
				return errMessage
			}
		}
		return ""
	}
}

// AssertGreater is an Assertion that checks whether the given value is greater
// than the provided limit.
func AssertGreater[T cmp.Ordered](lower T) Assertion[T] {
	return func(val T) string {
		if val > lower {
			return ""
		}
		return fmt.Sprintf("should be greater than %v", lower)
	}
}

// AssertGreaterEq is an Assertion that checks whether the given value is greater
// or equal to the provided limit.
func AssertGreaterEq[T cmp.Ordered](lower T) Assertion[T] {
	return func(val T) string {
		if val >= lower {
			return ""
		}
		return fmt.Sprintf("should be greater or equal %v", lower)
	}
}

// AssertLess is an Assertion that checks whether the given value is less
// than the provided limit.
func AssertLess[T cmp.Ordered](lower T) Assertion[T] {
	return func(val T) string {
		if val < lower {
			return ""
		}
		return fmt.Sprintf("should be less than %v", lower)
	}
}

// AssertLessEq is an Assertion that checks whether the given value is less
// or equal to the provided limit.
func AssertLessEq[T cmp.Ordered](lower T) Assertion[T] {
	return func(val T) string {
		if val <= lower {
			return ""
		}
		return fmt.Sprintf("should be less or equal %v", lower)
	}
}

// ForField checks the given Assertion-list on the provided value and reports the
// first encountered error, if any, to the Reporter.
func ForField[T any](reporter *Reporter, path *Path, val T, assertion Assertion[T], moreAssertions ...Assertion[T]) {
	assertions := append([]Assertion[T]{assertion}, moreAssertions...)
	reporter.NextField(path, val)
	for _, assertion := range assertions {
		errMessage := assertion(val)
		if errMessage != "" {
			reporter.Error(errMessage)
			return
		}
	}
}

// ForReporter checks the given Assertion-list on the provided value and reports the
// first encountered error, if any, to the Reporter.
func ForReporter[T any](reporter *Reporter, val T, assertion Assertion[T], moreAssertions ...Assertion[T]) {
	assertions := append([]Assertion[T]{assertion}, moreAssertions...)
	reporter.NextField(reporter.fieldPath, val)
	for _, assertion := range assertions {
		errMessage := assertion(val)
		if errMessage != "" {
			reporter.Error(errMessage)
			return
		}
	}
}

package validate

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"testing"
)

func TestReporter(t *testing.T) {
	r := NewReporter()
	require.NotNil(t, r, "should have created reporter")

	fieldPath := field.NewPath("aloud")
	r.NextField(fieldPath, 123)
	require.Empty(t, r.Report().Errors, "should have logged no errors")
	r.Error("my error message")
	require.NotEmpty(t, r.Report().Errors, "should have logged error")
	require.Contains(t, r.Report().Errors[0].Detail, "my error message")
	require.Equal(t, r.Report().Errors[0].Field, fieldPath.String())

	r.NextField(field.NewPath("whom"), "abc")
	require.Len(t, r.Report().Errors, 1, "should not have logged new error")
	r.Error("worth")
	r.Error("urge")
	require.Len(t, r.Report().Errors, 3, "should have logged all errors")
}

func TestErrorReporter_AddErrorList(t *testing.T) {
	r := NewReporter()

	r.NextField(field.NewPath("whom"), "abc")
	r.Error("worth")
	r.Error("urge")
	r.NextField(field.NewPath("none"), 123)
	r.Error("hello")

	otherReporter := NewReporter()
	otherReporter.NextField(field.NewPath("compare"), "average")
	otherReporter.Error("coal")
	otherReporter.Error("turn")
	otherErrList := otherReporter.Report()

	r.AddReport(otherErrList)

	assert.Len(t, r.Report().Errors, 5)
}

package entityvalidation

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
)

// ReportIsOKSuite tests Report.IsOK.
type ReportIsOKSuite struct {
	suite.Suite
	report Report
}

func (suite *ReportIsOKSuite) SetupTest() {
	suite.report = NewReport()
}

func (suite *ReportIsOKSuite) TestNotOK() {
	suite.report.Errors = append(suite.report.Errors, "sad life")
	suite.False(suite.report.IsOK(), "should not be ok")
}

func (suite *ReportIsOKSuite) TestOK() {
	suite.True(suite.report.IsOK(), "should be ok")
}

func TestReport_IsOK(t *testing.T) {
	suite.Run(t, new(ReportIsOKSuite))
}

func TestReport_AddError(t *testing.T) {
	report := NewReport()
	for i := 0; i < 8; i++ {
		report.AddError(fmt.Sprintf("meow%d", i))
		assert.Len(t, report.Errors, i+1, "should have added error")
	}
}

func TestReport_Include(t *testing.T) {
	report := NewReport()
	report.AddError("1")
	report.AddError("2")
	toInclude := NewReport()
	toInclude.AddError("3")
	toInclude.AddError("4")
	report.Include(toInclude)
	assert.Equal(t, []string{"1", "2", "3", "4"}, report.Errors, "should have included errors")
}

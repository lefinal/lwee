package validate

// IPv4Regex is a regex that matches IPv4 addresses.
const IPv4Regex = `(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])` +
	`\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])` +
	`\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])` +
	`\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])`

// Reporter is used as syntactic sugar in validation. Set the next field using
// NextField and then report errors with Report. The final error list can be
// retrieved via ErrorList.
type Reporter struct {
	fieldPath  *Path
	fieldValue any
	report     *Report
}

type Issue struct {
	Field    string
	BadValue any
	Detail   string
}

func NewIssue(field *Path, badValue any, detail string) Issue {
	return Issue{
		Field:    field.String(),
		BadValue: badValue,
		Detail:   detail,
	}
}

type Report struct {
	Warnings []Issue
	Errors   []Issue
}

func NewReport() *Report {
	return &Report{
		Warnings: make([]Issue, 0),
		Errors:   make([]Issue, 0),
	}
}

// AddWarning the given warning for the last field that was set via NextField.
func (r *Report) AddWarning(issue Issue) {
	r.Warnings = append(r.Warnings, issue)
}

// AddError the given error for the last field that was set via NextField.
func (r *Report) AddError(issue Issue) {
	r.Errors = append(r.Errors, issue)
}

func (r *Report) AddReport(otherReport *Report) {
	r.Warnings = append(r.Warnings, otherReport.Warnings...)
	r.Errors = append(r.Errors, otherReport.Errors...)
}

// NextField sets the field that calls to Error and Warn will use.
func (r *Reporter) NextField(fieldPath *Path, fieldValue any) {
	r.fieldPath = fieldPath
	r.fieldValue = fieldValue
}

// Warn the given warning for the last field that was set via NextField.
func (r *Reporter) Warn(warnMsg string) {
	r.report.Warnings = append(r.report.Warnings, NewIssue(r.fieldPath, r.fieldValue, warnMsg))
}

// Error the given error for the last field that was set via NextField.
func (r *Reporter) Error(errMsg string) {
	r.report.Errors = append(r.report.Errors, NewIssue(r.fieldPath, r.fieldValue, errMsg))
}

// AddReport adds the given Report.
func (r *Reporter) AddReport(otherReport *Report) {
	r.report.AddReport(otherReport)
}

// Report returns the final Report that contains all issues.
func (r *Reporter) Report() *Report {
	return r.report
}

// NewReporter creates a new Reporter that is ready to use.
func NewReporter() *Reporter {
	return &Reporter{
		fieldPath:  nil,
		fieldValue: nil,
		report:     NewReport(),
	}
}

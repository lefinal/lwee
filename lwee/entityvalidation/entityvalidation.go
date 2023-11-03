package entityvalidation

// Validatable can be validated.
type Validatable interface {
	// Validate the entity.
	Validate() (Report, error)
}

// Report of validation.
type Report struct {
	// Errors is a list of found errors during validation.
	Errors []string
}

// NewReport creates a new Report.
func NewReport() Report {
	return Report{}
}

// IsOK describes whether the Report does not contain any errors.
func (report *Report) IsOK() bool {
	return len(report.Errors) == 0
}

// AddError adds the given error message to the Report.
func (report *Report) AddError(message string) {
	report.Errors = append(report.Errors, message)
}

// Include errors of the given Report.
func (report *Report) Include(toInclude Report) {
	report.Errors = append(report.Errors, toInclude.Errors...)
}

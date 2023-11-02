package entityvalidation

import "fmt"

type Reporter struct {
	currentDescriptor string
	errList           []string
}

func NewReporter() *Reporter {
	return &Reporter{
		currentDescriptor: "",
		errList:           make([]string, 0),
	}
}

func (reporter *Reporter) Next(descriptor string) {
	reporter.currentDescriptor = descriptor
}

func (reporter *Reporter) ClearDescriptor() {
	reporter.currentDescriptor = ""
}

func (reporter *Reporter) Report(errMessage string) {
	if reporter.currentDescriptor != "" {
		errMessage = fmt.Sprintf("%s: %s", reporter.currentDescriptor, errMessage)
	}
	reporter.errList = append(reporter.errList, errMessage)
}

func (reporter *Reporter) Finalize() Report {
	return Report{
		Errors: reporter.errList,
	}
}

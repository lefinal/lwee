// Package lweeflowfile provides data structured and parsing methods for flow
// files in LWEE.
package lweeflowfile

import (
	"encoding/json"
	"github.com/lefinal/lwee/lwee/fileparse"
	"github.com/lefinal/lwee/lwee/validate"
	"github.com/lefinal/meh"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"os"
	"path/filepath"
	k8syaml "sigs.k8s.io/yaml"
	"strings"
)

// Flow represents an LWEE workflow file.
type Flow struct {
	// Raw representation of the file.
	Raw map[string]any
	// Name of the flow.
	Name string `json:"name"`
	// Description of the flow.
	Description string            `json:"description"`
	Inputs      FlowInputs        `json:"in"`
	Actions     map[string]Action `json:"actions"`
	Outputs     FlowOutputs       `json:"out"`
}

// Validate checks the validity of a Flow instance. It returns a validate.Report
// object containing any warnings or errors found during validation. The
// validation process includes checking the flow name and input, action, and
// output objects for any required fields that are empty. It also invokes the
// Validate method for each input, action, and output object to perform their
// respective validations.
func (flow *Flow) Validate() *validate.Report {
	reporter := validate.NewReporter()
	validate.ForField(reporter, field.NewPath("name"), flow.Name,
		validate.AssertNotEmpty[string]())
	for inputName, input := range flow.Inputs {
		reporter.AddReport(input.Validate(field.NewPath("in").Key(inputName)))
	}
	for actionName, action := range flow.Actions {
		reporter.AddReport(action.Validate(field.NewPath("actions").Key(actionName)))
	}
	for outputName, output := range flow.Outputs {
		reporter.AddReport(output.Validate(field.NewPath("out").Key(outputName)))
	}
	return reporter.Report()
}

// FlowInputType describes the type of flow input.
type FlowInputType string

const (
	// FlowInputTypeFile for FlowInputFile.
	FlowInputTypeFile FlowInputType = "file"
)

// FlowInput is used for ingesting data into a flow.
type FlowInput interface {
	Type() string
	Validate(path *validate.Path) *validate.Report
}

func flowInputConstructor[T FlowInput](t T) FlowInput {
	return t
}

// FlowInputs is a map for FlowInput that uses custom JSON unmarshalling logic.
type FlowInputs map[string]FlowInput

// UnmarshalJSON parses JSON data as FlowInputs. It customizes the JSON
// unmarshalling process by parsing the given JSON data based on the type, using
// the provided type mapping and type field name. It returns an error if the
// unmarshalling process fails or if it encounters unsupported types. The
// type-specific parsing is done using fileparse.ParseMapBasedOnType.
func (in *FlowInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = fileparse.ParseMapBasedOnType(data, map[FlowInputType]fileparse.Unmarshaller[FlowInput]{
		FlowInputTypeFile: fileparse.UnmarshallerFn[FlowInputFile](flowInputConstructor[FlowInputFile]),
	}, "providedAs")
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

// FlowInputFile describes a flow input that reads from a file.
type FlowInputFile struct {
	Filename string `json:"filename"`
}

// Type of FlowInputFile.
func (input FlowInputFile) Type() string {
	return string(FlowInputTypeFile)
}

// Validate the options.
func (input FlowInputFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.NextField(path.Child("filename"), input.Filename)
	validate.ForReporter(reporter, input.Filename,
		validate.AssertNotEmpty[string]())
	if input.Filename != "" && filepath.IsAbs(input.Filename) {
		reporter.Warn("absolute paths make it more difficult to run the same flow in other environments")
	}
	return reporter.Report()
}

// FlowOutputType is the type of flow output.
type FlowOutputType string

const (
	// FlowOutputTypeFile for FlowOutputFile.
	FlowOutputTypeFile FlowOutputType = "file"
)

// FlowOutput allows preserving artifacts from a flow.
type FlowOutput interface {
	Type() string
	Validate(path *validate.Path) *validate.Report
}

func flowOutputConstructor[T FlowOutput](t T) FlowOutput {
	return t
}

// FlowOutputs is a map for FlowOutput that uses custom JSON unmarshalling logic.
type FlowOutputs map[string]FlowOutput

// UnmarshalJSON parses JSON data as FlowOutput. It customizes the JSON
// unmarshalling process by parsing the given JSON data based on the type, using
// the provided type mapping and type field name. It returns an error if the
// unmarshalling process fails or if it encounters unsupported types. The
// type-specific parsing is done using fileparse.ParseMapBasedOnType.
func (out *FlowOutputs) UnmarshalJSON(data []byte) error {
	var err error
	*out, err = fileparse.ParseMapBasedOnType(data, map[FlowOutputType]fileparse.Unmarshaller[FlowOutput]{
		FlowOutputTypeFile: fileparse.UnmarshallerFn[FlowOutputFile](flowOutputConstructor[FlowOutputFile]),
	}, "provideAs")
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

// FlowOutputBase holds common fields for FlowOutput.
type FlowOutputBase struct {
	Source string `json:"source"`
}

func (base FlowOutputBase) validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.NextField(path.Child("source"), base.Source)
	validate.ForReporter(reporter, base.Source,
		validate.AssertNotEmpty[string]())
	if strings.Contains(base.Source, " ") {
		reporter.Warn("spaces in source may lead to confusion in log output. consider renaming your source entities.")
	}
	return reporter.Report()
}

// FlowOutputFile provides generated flow artifacts as file.
type FlowOutputFile struct {
	FlowOutputBase
	Filename string `json:"filename"`
}

// Type of FlowOutputFile.
func (output FlowOutputFile) Type() string {
	return string(FlowOutputTypeFile)
}

// Validate the options.
func (output FlowOutputFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(output.FlowOutputBase.validate(path))
	validate.ForField(reporter, path.Child("filename"), output.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ParseFlow parses the given JSON representation as Flow.
func ParseFlow(rawFlow json.RawMessage) (Flow, error) {
	var flow Flow
	err := json.Unmarshal(rawFlow, &flow)
	if err != nil {
		return Flow{}, meh.NewBadInputErrFromErr(err, "unmarshal flow", nil)
	}
	m := make(map[string]any)
	err = json.Unmarshal(rawFlow, &m)
	if err != nil {
		return Flow{}, meh.NewBadInputErrFromErr(err, "parse raw flow into map", nil)
	}
	flow.Raw = m
	return flow, nil
}

// FromFile reads a file and parses it as Flow.
func FromFile(filename string) (Flow, error) {
	// Read file contents.
	rawFlow, err := os.ReadFile(filename)
	if err != nil {
		return Flow{}, meh.NewBadInputErrFromErr(err, "read flow", nil)
	}
	var rawFlowJSON json.RawMessage
	// If YAML, we need to convert to JSON.
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		rawFlowJSON, err = k8syaml.YAMLToJSON(rawFlow)
		if err != nil {
			return Flow{}, meh.Wrap(err, "yaml to json", nil)
		}
	} else if strings.HasSuffix(filename, ".json") {
		rawFlowJSON = rawFlow
	} else {
		return Flow{}, meh.NewBadInputErr("unsupported file extension", nil)
	}
	// Parse.
	flow, err := ParseFlow(rawFlowJSON)
	if err != nil {
		return Flow{}, meh.Wrap(err, "parse flow", nil)
	}
	return flow, nil
}

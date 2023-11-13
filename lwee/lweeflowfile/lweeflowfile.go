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

type Flow struct {
	Raw         map[string]any
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Inputs      FlowInputs        `json:"in"`
	Actions     map[string]Action `json:"actions"`
	Outputs     FlowOutputs       `json:"out"`
}

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

type FlowInputType string

const (
	FlowInputTypeFile FlowInputType = "file"
)

type FlowInput interface {
	Type() string
	Validate(path *validate.Path) *validate.Report
}

func flowInputConstructor[T FlowInput](t T) FlowInput {
	return t
}

type FlowInputs map[string]FlowInput

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

type FlowInputFile struct {
	Filename string `json:"filename"`
}

func (input FlowInputFile) Type() string {
	return string(FlowInputTypeFile)
}

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

type FlowOutputType string

const (
	FlowOutputTypeFile FlowOutputType = "file"
)

type FlowOutput interface {
	SourceName() string
	Type() string
	Validate(path *validate.Path) *validate.Report
}

func flowOutputConstructor[T FlowOutput](t T) FlowOutput {
	return t
}

type FlowOutputs map[string]FlowOutput

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

type FlowOutputBase struct {
	Source string `json:"source"`
}

func (base FlowOutputBase) SourceName() string {
	return base.Source
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

type FlowOutputFile struct {
	FlowOutputBase
	Filename string `json:"filename"`
}

func (output FlowOutputFile) Type() string {
	return string(FlowOutputTypeFile)
}

func (output FlowOutputFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(output.FlowOutputBase.validate(path))
	validate.ForField(reporter, path.Child("filename"), output.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

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

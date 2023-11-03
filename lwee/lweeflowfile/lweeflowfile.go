package lweeflowfile

import (
	"encoding/json"
	"github.com/lefinal/lwee/lwee/lweefile"
	"github.com/lefinal/meh"
	"os"
	k8syaml "sigs.k8s.io/yaml"
	"strings"
)

type Flow struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Inputs      FlowInputs        `json:"in"`
	Actions     map[string]Action `json:"actions"`
	Outputs     FlowOutputs       `json:"out"`
}

type FlowInputType string

const (
	FlowInputTypeFile FlowInputType = "file"
)

type FlowInput interface {
	Type() string
}

func flowInputConstructor[T FlowInput](t T) FlowInput {
	return t
}

type FlowInputs map[string]FlowInput

func (in *FlowInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = lweefile.ParseMapBasedOnType(data, map[FlowInputType]lweefile.Unmarshaller[FlowInput]{
		FlowInputTypeFile: lweefile.UnmarshallerFn[FlowInputFile](flowInputConstructor[FlowInputFile]),
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

type FlowOutputType string

const (
	FlowOutputTypeFile FlowOutputType = "file"
)

type FlowOutput interface {
	SourceName() string
	Type() string
}

func flowOutputConstructor[T FlowOutput](t T) FlowOutput {
	return t
}

type FlowOutputs map[string]FlowOutput

func (out *FlowOutputs) UnmarshalJSON(data []byte) error {
	var err error
	*out, err = lweefile.ParseMapBasedOnType(data, map[FlowOutputType]lweefile.Unmarshaller[FlowOutput]{
		FlowOutputTypeFile: lweefile.UnmarshallerFn[FlowOutputFile](flowOutputConstructor[FlowOutputFile]),
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

type FlowOutputFile struct {
	FlowOutputBase
	Filename string `json:"filename"`
}

func (output FlowOutputFile) Type() string {
	return string(FlowOutputTypeFile)
}

func ParseFlow(rawFlow json.RawMessage) (Flow, error) {
	var flow Flow
	err := json.Unmarshal(rawFlow, &flow)
	if err != nil {
		return Flow{}, meh.NewBadInputErrFromErr(err, "unmarshal flow", nil)
	}
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

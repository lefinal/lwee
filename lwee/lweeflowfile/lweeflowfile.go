package lweeflowfile

import (
	"encoding/json"
	"github.com/lefinal/lwee/lwee/lweefile"
	"github.com/lefinal/meh"
	"os"
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

type FlowInputs map[string]any

func (in *FlowInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = lweefile.ParseMapBasedOnType(data, map[FlowInputType]any{
		FlowInputTypeFile: FlowInputFile{},
	})
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

type FlowInputFile struct {
	Filepath string `json:"filepath"`
}

type FlowOutputType string

const (
	FlowOutputTypeFile FlowOutputType = "file"
)

type FlowOutputs map[string]any

func (out *FlowOutputs) UnmarshalJSON(data []byte) error {
	var err error
	*out, err = lweefile.ParseMapBasedOnType(data, map[FlowOutputType]any{
		FlowOutputTypeFile: FlowOutputFile{},
	})
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

type FlowOutputBase struct {
	Source string `json:"source"`
}

type FlowOutputFile struct {
	FlowOutputBase
	Filename string `json:"filename"`
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
		rawFlowJSON, err = lweefile.YAMLToJSON(rawFlowJSON)
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

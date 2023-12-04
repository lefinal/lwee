// Package lweeprojactionfile holds relevant data structures and parsing methods
// for project actions in LWEE.
package lweeprojactionfile

import (
	"encoding/json"
	"github.com/lefinal/lwee/lwee/fileparse"
	"github.com/lefinal/meh"
	"os"
	k8syaml "sigs.k8s.io/yaml"
	"strings"
)

// ProjActionType describes the type of project action. Currently, only
// ProjActionTypeImage is supported.
type ProjActionType string

const (
	// ProjActionTypeImage for ProjActionConfigImage.
	ProjActionTypeImage ProjActionType = "image"
)

// ProjAction is the provided information of a project action. This is the
// action.yaml file in a project action's directory.
type ProjAction struct {
	Configs ProjActionConfigs `json:"configs"`
}

// ProjActionConfig provides useful information for further handling of a project
// action.
type ProjActionConfig interface {
	Type() string
}

func projActionConfigConstructor[T ProjActionConfig](t T) ProjActionConfig {
	return t
}

// ProjActionConfigs is a map of ProjActionConfig that uses custom JSON
// unmarshalling logic based on the config's type.
type ProjActionConfigs map[string]ProjActionConfig

// UnmarshalJSON parses the JSON data as ProjActionConfigs based on their type.
func (config *ProjActionConfigs) UnmarshalJSON(data []byte) error {
	var err error
	*config, err = fileparse.ParseMapBasedOnType[ProjActionType, ProjActionConfig](data, map[ProjActionType]fileparse.Unmarshaller[ProjActionConfig]{
		ProjActionTypeImage: fileparse.UnmarshallerFn[ProjActionConfigImage](projActionConfigConstructor[ProjActionConfigImage]),
	}, "type")
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

// ProjActionConfigImage describes a containerized project action.
type ProjActionConfigImage struct {
	File string `json:"file"`
}

// Type of ProjActionConfigImage.
func (config ProjActionConfigImage) Type() string {
	return string(ProjActionTypeImage)
}

// ParseAction parses the given JSON data as ProjAction.
func ParseAction(rawAction json.RawMessage) (ProjAction, error) {
	var action ProjAction
	err := json.Unmarshal(rawAction, &action)
	if err != nil {
		return ProjAction{}, meh.NewBadInputErrFromErr(err, "unmarshal project action", nil)
	}
	return action, nil
}

// FromFile reads the project action config from the given file and parses it as
// ProjAction.
func FromFile(filename string) (ProjAction, error) {
	// Read file contents.
	rawAction, err := os.ReadFile(filename)
	if err != nil {
		return ProjAction{}, meh.NewBadInputErrFromErr(err, "read project action", nil)
	}
	var rawActionJSON json.RawMessage
	// If YAML, we need to convert to JSON.
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		rawActionJSON, err = k8syaml.YAMLToJSON(rawAction)
		if err != nil {
			return ProjAction{}, meh.Wrap(err, "yaml to json", nil)
		}
	} else if strings.HasSuffix(filename, ".json") {
		rawActionJSON = rawAction
	} else {
		return ProjAction{}, meh.NewBadInputErr("unsupported file extension", nil)
	}
	// Parse.
	action, err := ParseAction(rawActionJSON)
	if err != nil {
		return ProjAction{}, meh.Wrap(err, "parse project action", nil)
	}
	return action, nil
}

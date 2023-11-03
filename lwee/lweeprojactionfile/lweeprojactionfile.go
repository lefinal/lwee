package lweeprojactionfile

import (
	"encoding/json"
	"github.com/lefinal/lwee/lwee/lweefile"
	"github.com/lefinal/meh"
	"os"
	k8syaml "sigs.k8s.io/yaml"
	"strings"
)

type ProjActionType string

const (
	ProjActionTypeImage ProjActionType = "image"
)

type ProjAction struct {
	Configs ProjActionConfigs `json:"configs"`
}

type ProjActionConfig interface {
	Type() string
}

func projActionConfigConstructor[T ProjActionConfig](t T) ProjActionConfig {
	return t
}

type ProjActionConfigs map[string]ProjActionConfig

func (config *ProjActionConfigs) UnmarshalJSON(data []byte) error {
	var err error
	*config, err = lweefile.ParseMapBasedOnType[ProjActionType, ProjActionConfig](data, map[ProjActionType]lweefile.Unmarshaller[ProjActionConfig]{
		ProjActionTypeImage: lweefile.UnmarshallerFn[ProjActionConfigImage](projActionConfigConstructor[ProjActionConfigImage]),
	}, "type")
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

type ProjActionConfigImage struct {
	File string `json:"file"`
}

func (config ProjActionConfigImage) Type() string {
	return string(ProjActionTypeImage)
}

func ParseAction(rawAction json.RawMessage) (ProjAction, error) {
	var action ProjAction
	err := json.Unmarshal(rawAction, &action)
	if err != nil {
		return ProjAction{}, meh.NewBadInputErrFromErr(err, "unmarshal project action", nil)
	}
	return action, nil
}

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

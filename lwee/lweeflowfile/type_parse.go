package lweeflowfile

import (
	"encoding/json"
	"fmt"
	"github.com/lefinal/meh"
)

type typeBase[T any] struct {
	Type T `json:"type"`
}

func parseMapBasedOnType[T comparable](data []byte, typeMapping map[T]any) (map[string]any, error) {
	m := make(map[string]any)
	// Parse raw JSON.
	var rawJSONMap map[string]json.RawMessage
	err := json.Unmarshal(data, &rawJSONMap)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "unmarshal raw json map", nil)
	}
	// Parse type and then the final type.
	for k, rawJSON := range rawJSONMap {
		m[k], err = parseBasedOnType(rawJSON, typeMapping)
		if err != nil {
			return nil, meh.Wrap(err, fmt.Sprintf("parse %q based on type", k), nil)
		}
	}
	return m, nil
}

func parseBasedOnType[T comparable](data []byte, typeMapping map[T]any) (any, error) {
	var typeBase typeBase[T]
	err := json.Unmarshal(data, &typeBase)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "unmarshal type base", nil)
	}
	actualType, ok := typeMapping[typeBase.Type]
	if !ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported flow: %v", typeBase.Type), nil)
	}
	err = json.Unmarshal(data, &actualType)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "parse actual type", nil)
	}
	return actualType, nil
}

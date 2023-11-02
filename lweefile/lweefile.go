package lweefile

import (
	"encoding/json"
	"fmt"
	"github.com/lefinal/meh"
)

type typeBase[T any] struct {
	Type T `json:"type"`
}

func UnmarshallerFn[T any]() Unmarshaller {
	return func(data []byte) (any, error) {
		var t T
		err := json.Unmarshal(data, &t)
		return t, err
	}
}

type Unmarshaller func(data []byte) (any, error)

func ParseMapBasedOnType[T ~string](data []byte, typeMapping map[T]Unmarshaller, typeFieldName string) (map[string]any, error) {
	m := make(map[string]any)
	// Parse raw JSON.
	var rawJSONMap map[string]json.RawMessage
	err := json.Unmarshal(data, &rawJSONMap)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "unmarshal raw json map", nil)
	}
	// Parse type and then the final type.
	for k, rawJSON := range rawJSONMap {
		m[k], err = ParseBasedOnType(rawJSON, typeMapping, typeFieldName)
		if err != nil {
			return nil, meh.Wrap(err, fmt.Sprintf("parse %q based on type", k), nil)
		}
	}
	return m, nil
}

func ParseBasedOnType[T ~string](data []byte, typeMapping map[T]Unmarshaller, typeFieldName string) (any, error) {
	var typeBase map[string]any
	err := json.Unmarshal(data, &typeBase)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "unmarshal type base", nil)
	}
	typeNameRaw := typeBase[typeFieldName]
	if typeNameRaw == nil || typeNameRaw == "" {
		return nil, meh.NewBadInputErr("missing type name", meh.Details{"type_field_name": typeFieldName})
	}
	typeNameStr, ok := typeNameRaw.(string)
	if !ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("type name has unexpected data type: %T", typeNameRaw),
			meh.Details{"type_name_was": typeNameRaw})
	}
	typeName := T(typeNameStr)
	_, ok = typeMapping[typeName]
	if !ok {
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported type: %v", typeName), nil)
	}
	parsed, err := typeMapping[typeName](data)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "parse actual type", nil)
	}
	return parsed, nil
}

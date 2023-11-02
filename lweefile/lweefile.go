package lweefile

import (
	"encoding/json"
	"fmt"
	"github.com/lefinal/meh"
)

type typeBase[T any] struct {
	Type T `json:"type"`
}

func UnmarshallerFn[T any, S any](constructorFn func(t T) S) Unmarshaller[S] {
	return func(data []byte) (S, error) {
		var t T
		err := json.Unmarshal(data, &t)
		return constructorFn(t), err
	}
}

type Unmarshaller[S any] func(data []byte) (S, error)

func ParseMapBasedOnType[T ~string, S any](data []byte, typeMapping map[T]Unmarshaller[S], typeFieldName string) (map[string]S, error) {
	m := make(map[string]S)
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

func ParseBasedOnType[T ~string, S any](data []byte, typeMapping map[T]Unmarshaller[S], typeFieldName string) (S, error) {
	var s S
	var typeBase map[string]any
	err := json.Unmarshal(data, &typeBase)
	if err != nil {
		return s, meh.NewBadInputErrFromErr(err, "unmarshal type base", nil)
	}
	typeNameRaw := typeBase[typeFieldName]
	if typeNameRaw == nil || typeNameRaw == "" {
		return s, meh.NewBadInputErr("missing type name", meh.Details{"type_field_name": typeFieldName})
	}
	typeNameStr, ok := typeNameRaw.(string)
	if !ok {
		return s, meh.NewBadInputErr(fmt.Sprintf("type name has unexpected data type: %T", typeNameRaw),
			meh.Details{"type_name_was": typeNameRaw})
	}
	typeName := T(typeNameStr)
	_, ok = typeMapping[typeName]
	if !ok {
		return s, meh.NewBadInputErr(fmt.Sprintf("unsupported type: %v", typeName), nil)
	}
	parsed, err := typeMapping[typeName](data)
	if err != nil {
		return s, meh.NewBadInputErrFromErr(err, "parse actual type", nil)
	}
	return parsed, nil
}

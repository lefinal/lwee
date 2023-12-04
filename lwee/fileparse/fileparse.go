// Package fileparse is used for parsing flow files or any others. It supports
// helpers like ParseBasedOnType which are frequently used throughout the
// project.
package fileparse

import (
	"encoding/json"
	"fmt"
	"github.com/lefinal/meh"
)

// UnmarshallerFn is a higher-order function that creates a JSON Unmarshaller
// function for a specific type.
func UnmarshallerFn[T any, S any](constructorFn func(t T) S) Unmarshaller[S] {
	return func(data []byte) (S, error) {
		var t T
		err := json.Unmarshal(data, &t)
		return constructorFn(t), err
	}
}

// Unmarshaller is a generic type alias for a function that unmarshals JSON data
// into a value of type S.
type Unmarshaller[S any] func(data []byte) (S, error)

// ParseMapBasedOnType parses a JSON map based on a specified type field name,
// using the provided type mapping to create the corresponding values. It returns
// a map where the values are the parsed objects of type S. If an error occurs
// during parsing or if a type is not supported, it returns an error.
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
			return nil, meh.Wrap(err, fmt.Sprintf("parse %q based on type", k), meh.Details{"raw_map": rawJSONMap})
		}
	}
	return m, nil
}

// ParseBasedOnType is a function that parses JSON data based on the type field.
// It accepts the JSON data, a mapping of type name to Unmarshaller, and the type
// field name. It returns the parsed object of the corresponding type and an
// error if any.
func ParseBasedOnType[T ~string, S any](data []byte, typeMapping map[T]Unmarshaller[S], typeFieldName string) (S, error) {
	var s S
	var raw map[string]any
	err := json.Unmarshal(data, &raw)
	if err != nil {
		return s, meh.NewBadInputErrFromErr(err, "unmarshal type base", meh.Details{"tried_to_unmarshal_type_base": string(data)})
	}
	typeNameRaw := raw[typeFieldName]
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

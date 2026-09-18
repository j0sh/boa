package boa

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
)

// UnmarshalJSON decodes JSON using encoding/json compatibility semantics and
// Boa's registered parsers for string values. Native JSON values and custom
// unmarshal methods remain under the JSON decoder's control. A custom method
// that delegates decoding can call UnmarshalJSON to retain Boa's parsers.
func UnmarshalJSON(data []byte, target any) error {
	return jsonv2.Unmarshal(data, target, json.DefaultOptionsV1(), jsonv2.WithUnmarshalers(JSONUnmarshalers()))
}

// JSONUnmarshalers provides the same string parsers for decoders configured
// with encoding/json/v2 options, such as RejectUnknownMembers.
func JSONUnmarshalers() *jsonv2.Unmarshalers { return configStringParsers }

var configStringParsers = jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, target any) error {
	dst := reflect.ValueOf(target).Elem()
	handler := exactTypeHandlers[dst.Type()]
	if handler == nil || dec.PeekKind() != '"' {
		return errors.ErrUnsupported
	}
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}
	parsed, err := handler.parse(string(dec.StackPointer()), token.String())
	if err != nil {
		return err
	}
	dst.Set(reflect.ValueOf(parsed).Elem())
	return nil
})

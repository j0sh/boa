package boa

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// jsonKeyTree retains the union of object keys across repeated members. Decoding
// into map[string]any would discard earlier objects that a struct decoder merges.
// Values are deliberately discarded; nulls and scalars must not erase prior keys.
func jsonKeyTree(data []byte) (map[string]any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var read func(map[string]any) (map[string]any, error)
	read = func(out map[string]any) (map[string]any, error) {
		if dec.PeekKind() != '{' {
			_, err := dec.ReadValue()
			return out, err
		}
		if out == nil {
			out = map[string]any{}
		}
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		for dec.PeekKind() != '}' {
			key, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			name := key.String()
			out[name], err = read(asKeyMap(out[name]))
			if err != nil {
				return nil, err
			}
		}
		_, err := dec.ReadToken()
		return out, err
	}
	out, err := read(nil)
	if err == nil {
		if _, err = dec.ReadToken(); err == io.EOF {
			err = nil
		} else if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
	}
	return out, err
}

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

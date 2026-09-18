package boa

import (
	"encoding"
	"fmt"
	"reflect"
)

// Text adapts a value to encoding.TextUnmarshaler for config decoders. It uses
// the same parser as Boa's CLI flags, environment variables, and default tags.
// For example, Text[time.Duration] accepts "2.5h" in JSON, YAML, or TOML when
// the decoder supports encoding.TextUnmarshaler. Access the value through Value.
// For types you own, implement encoding.TextUnmarshaler directly instead.
type Text[T any] struct{ Value T }

func (v *Text[T]) UnmarshalText(data []byte) error {
	handler := resolveHandler(reflect.TypeFor[T]())
	if handler == nil {
		return fmt.Errorf("boa: no text parser for %T", v.Value)
	}
	parsed, err := handler.parse("value", string(data))
	if err != nil {
		return err
	}
	v.Value = reflect.ValueOf(parsed).Elem().Convert(reflect.TypeFor[T]()).Interface().(T)
	return nil
}

func (v Text[T]) MarshalText() ([]byte, error) {
	if handler := lookupHandler(reflect.TypeFor[T]()); handler != nil && handler.format != nil {
		return []byte(handler.format(reflect.ValueOf(&v.Value).Elem())), nil
	}
	if marshaler, ok := any(&v.Value).(encoding.TextMarshaler); ok {
		return marshaler.MarshalText()
	}
	return fmt.Append(nil, v.Value), nil
}

func (v Text[T]) String() string { text, _ := v.MarshalText(); return string(text) }

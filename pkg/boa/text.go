package boa

import (
	"encoding"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
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

// HTTPBind is an optional HTTP listener address embedding netip.AddrPort.
// Text input accepts IP:port or [IPv6]:port with ports between 1 and 65535;
// :port uses 127.0.0.1, and empty input clears the address (IsValid reports false).
// Hostnames are rejected. UnmarshalText validates syntax; Cmd enforces exposure
// policy. String returns the plain listener address.
type HTTPBind struct {
	netip.AddrPort
	acknowledged bool
}

// UnmarshalText parses a listener address, leaving b unchanged on error.
func (b *HTTPBind) UnmarshalText(text []byte) error {
	raw := string(text)
	if raw == "" {
		*b = HTTPBind{}
		return nil
	}
	raw, acknowledged := strings.CutPrefix(raw, "unsafe@")
	if strings.HasPrefix(raw, ":") {
		raw = "127.0.0.1" + raw
	}
	addr, err := netip.ParseAddrPort(raw)
	if err != nil {
		return err
	}
	if addr.Port() == 0 {
		return errors.New("TCP port must be between 1 and 65535")
	}
	*b = HTTPBind{AddrPort: addr, acknowledged: acknowledged}
	return nil
}

// UnmarshalYAML parses a scalar listener address using UnmarshalText.
func (b *HTTPBind) UnmarshalYAML(unmarshal func(any) error) error {
	var text string
	if err := unmarshal(&text); err != nil {
		return err
	}
	return b.UnmarshalText([]byte(text))
}

// AppendText appends b to text, preserving an explicitly supplied unsafe@ prefix.
func (b HTTPBind) AppendText(text []byte) ([]byte, error) {
	if b.IsValid() && b.acknowledged {
		text = append(text, "unsafe@"...)
	}
	return b.AddrPort.AppendText(text)
}

// MarshalText returns the same encoding as AppendText.
func (b HTTPBind) MarshalText() ([]byte, error) { return b.AppendText([]byte{}) }

func validateHTTPBinds(v reflect.Value, seen map[configCopyKey]bool) error {
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		key := configCopyKey{typ: v.Type(), ptr: v.UnsafePointer()}
		if v.Kind() == reflect.Slice {
			key.len = v.Len()
		}
		if v.IsNil() || seen[key] {
			return nil
		}
		seen[key] = true
	}
	if v.Type() == reflect.TypeFor[HTTPBind]() {
		b := v.Interface().(HTTPBind)
		if b.IsValid() && !b.Addr().IsLoopback() && !b.acknowledged {
			return fmt.Errorf("non-loopback HTTP bind %q requires explicit acknowledgment; use %q", b.String(), "unsafe@"+b.String())
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return validateHTTPBinds(v.Elem(), seen)
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		for _, elem := range v.Seq2() {
			if err := validateHTTPBinds(elem, seen); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Field(i).CanInterface() {
				if err := validateHTTPBinds(v.Field(i), seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

package boa

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Decode registered fields separately, keeping ordinary fields under the format's validation.
func unmarshalConfigWithExactTypes(data []byte, target any, format ConfigFormat) error {
	normalErr := format.Unmarshal(data, target)
	dst := reflect.ValueOf(target)
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return normalErr
	}
	var raw json.RawMessage
	jsonInput := format.Unmarshal(data, &raw) == nil && json.Valid(raw)
	t := configProxyType(dst.Elem().Type(), jsonInput, map[reflect.Type]bool{})
	if t == dst.Elem().Type() {
		return normalErr
	}
	proxy := reflect.New(t)
	_, _ = copyConfigProxy(dst.Elem(), proxy.Elem(), "", false, false)
	if err := format.Unmarshal(data, proxy.Interface()); err != nil {
		return normalErr
	}
	changed, err := copyConfigProxy(proxy.Elem(), dst.Elem(), "", true, normalErr != nil)
	if changed && err == nil {
		return nil
	}
	return errors.Join(normalErr, err)
}

func configProxyType(t reflect.Type, jsonInput bool, visiting map[reflect.Type]bool) reflect.Type {
	if exactTypeHandlers[t] != nil {
		if jsonInput {
			return reflect.TypeFor[json.RawMessage]()
		}
		return reflect.TypeFor[any]()
	}
	if visiting[t] || reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return t
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		elem := configProxyType(t.Elem(), jsonInput, visiting)
		if elem != t.Elem() {
			switch t.Kind() {
			case reflect.Pointer:
				return reflect.PointerTo(elem)
			case reflect.Slice:
				return reflect.SliceOf(elem)
			case reflect.Map:
				return reflect.MapOf(t.Key(), elem)
			}
		}
	case reflect.Struct:
		fields := make([]reflect.StructField, t.NumField())
		changed := false
		for i := range fields {
			fields[i] = t.Field(i)
			if fields[i].Anonymous && !fields[i].IsExported() {
				return t
			}
			if fields[i].IsExported() && !isBoaIgnored(fields[i]) {
				fields[i].Type = configProxyType(fields[i].Type, jsonInput, visiting)
				changed = changed || fields[i].Type != t.Field(i).Type
			}
		}
		if changed {
			return reflect.StructOf(fields)
		}
	}
	return t
}

func copyConfigProxy(src, dst reflect.Value, path string, parsing, native bool) (bool, error) {
	if src.Type() == dst.Type() {
		dst.Set(src)
		return false, nil
	}
	if !parsing && exactTypeHandlers[src.Type()] != nil {
		return false, nil
	}
	if h := exactTypeHandlers[dst.Type()]; parsing && h != nil {
		if src.IsNil() { // Omitted registered fields keep their existing values.
			return false, nil
		}
		value := src.Interface()
		if raw, ok := value.(json.RawMessage); ok {
			if len(raw) == 0 || raw[0] != '"' {
				if native {
					return false, json.Unmarshal(raw, dst.Addr().Interface())
				}
				return false, nil
			}
			if err := json.Unmarshal(raw, &value); err != nil {
				return false, err
			}
		}
		text, stringValue := value.(string)
		if !stringValue {
			if !native {
				return false, nil
			}
			// Preserve the TOML decoder's local timezone for dates and date-times.
			if date, ok := value.(interface {
				AsTime(*time.Location) time.Time
			}); ok && dst.Type() == timeType {
				value = date.AsTime(time.Local)
			}
			v := reflect.ValueOf(value)
			if v.Type().AssignableTo(dst.Type()) {
				dst.Set(v)
				return false, nil
			}
			integer := func(k reflect.Kind) bool { return k >= reflect.Int && k <= reflect.Uint64 }
			if kindHandlers[dst.Kind()] == nil || (v.Kind() != dst.Kind() && !(integer(v.Kind()) && integer(dst.Kind()))) {
				return false, fmt.Errorf("invalid config value for %s: cannot assign %T to %s", path, value, dst.Type())
			}
			h, text = kindHandlers[dst.Kind()], fmt.Sprint(value)
		}
		parsed, err := h.parse(path, text)
		if err == nil {
			v := reflect.ValueOf(parsed)
			if v.Type() != dst.Type() {
				v = v.Elem().Convert(dst.Type())
			}
			dst.Set(v)
		}
		return stringValue, err
	}
	if src.Kind() != reflect.Struct && src.IsNil() {
		dst.SetZero()
		return false, nil
	}
	if src.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return copyConfigProxy(src.Elem(), dst.Elem(), path, parsing, native)
	}
	changed := false
	copyValue := func(s, d reflect.Value, name string) error {
		updated, err := copyConfigProxy(s, d, path+name, parsing, native)
		changed = changed || updated
		return err
	}
	switch src.Kind() {
	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			if dst.Field(i).CanSet() {
				if raw, ok := src.Field(i).Interface().(json.RawMessage); native && ok && len(raw) > 0 && raw[0] != '"' && string(raw) != "null" && strings.Contains(dst.Type().Field(i).Tag.Get("json"), ",string") {
					return false, fmt.Errorf("invalid non-string config value for %s.%s", path, dst.Type().Field(i).Name)
				}
				if err := copyValue(src.Field(i), dst.Field(i), "."+src.Type().Field(i).Name); err != nil {
					return false, err
				}
			}
		}
	case reflect.Slice:
		values := reflect.MakeSlice(dst.Type(), src.Len(), src.Len())
		reflect.Copy(values, dst)
		dst.Set(values)
		for i := 0; i < src.Len(); i++ {
			if err := copyValue(src.Index(i), dst.Index(i), fmt.Sprintf("[%d]", i)); err != nil {
				return false, err
			}
		}
	case reflect.Map:
		if dst.IsNil() {
			dst.Set(reflect.MakeMap(dst.Type()))
		}
		for iter := src.MapRange(); iter.Next(); {
			value := reflect.New(dst.Type().Elem()).Elem()
			if current := dst.MapIndex(iter.Key()); current.IsValid() {
				value.Set(current)
			}
			if err := copyValue(iter.Value(), value, fmt.Sprint("[", iter.Key(), "]")); err != nil {
				return false, err
			}
			dst.SetMapIndex(iter.Key(), value)
		}
	}
	return changed, nil
}

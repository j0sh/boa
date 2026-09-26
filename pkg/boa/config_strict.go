package boa

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// checkConfigKeys checks names only. Values, custom decoding methods, and type
// conversions remain the responsibility of the selected target decoder.
func checkConfigKeys(raw any, t reflect.Type, tag, path string) error {
	if raw == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		object, err := strictConfigObject(raw, path)
		if err != nil || object == nil {
			return err
		}
		match := configFieldMatcher(t, tag)
		for _, key := range slices.Sorted(maps.Keys(object)) {
			field := match(key)
			if field == nil {
				return fmt.Errorf("unknown config field %q", configMemberPath(path, key))
			}
			if err := checkConfigKeys(object[key], field, tag, configMemberPath(path, key)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		value := reflect.ValueOf(raw)
		if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
			if configContainsObjects(t.Elem(), map[reflect.Type]bool{}) && !configScalarDecoder(t, tag) {
				return fmt.Errorf("cannot inspect config field %q with RejectUnknown: KeyTree must retain collection elements", path)
			}
			return nil
		}
		for i := 0; i < value.Len(); i++ {
			if err := checkConfigKeys(value.Index(i).Interface(), t.Elem(), tag, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// A custom collection decoder may accept a scalar representation. It has no
// member names to inspect, and its native method must retain control of parsing.
func configScalarDecoder(t reflect.Type, tag string) bool {
	if lookupHandler(t) != nil {
		return true
	}
	methods := map[string][]string{
		"json": {"UnmarshalJSON", "UnmarshalJSONFrom"},
		"yaml": {"UnmarshalYAML"},
		"toml": {"UnmarshalTOML"},
	}
	for _, name := range methods[tag] {
		if _, ok := reflect.PointerTo(t).MethodByName(name); ok {
			return true
		}
	}
	return false
}

// Resolve conflicts by exact name before considering case-insensitive matches.
// Index paths retain declaration order; folded matches use breadth-first order.
func configFieldMatcher(t reflect.Type, tag string) func(string) reflect.Type {
	if t.Kind() == reflect.Map {
		return func(string) reflect.Type { return t.Elem() }
	}
	type field struct {
		reflect.StructField
		tagged, fold, ambiguous bool
	}
	fields := map[string]field{}
	var inline reflect.Type
	for _, f := range configFields(t, tag, nil) {
		if len(f.keys) != 1 {
			continue
		}
		index := splitPath(f.path)
		sf := t.FieldByIndex(index)
		sf.Index = index
		parts := strings.Split(sf.Tag.Get(tag), ",")
		if tag == "yaml" && sf.Type.Kind() == reflect.Map && slices.Contains(parts[1:], "inline") {
			inline = sf.Type.Elem()
			continue
		}
		next := field{sf, parts[0] != "", tag != "yaml" && (tag != "json" || !slices.Contains(parts[1:], "case:strict")), false}
		name := f.keys[0]
		old, ok := fields[name]
		switch {
		case !ok || len(index) < len(old.Index) || len(index) == len(old.Index) && next.tagged && !old.tagged:
			fields[name] = next
		case len(index) == len(old.Index) && next.tagged == old.tagged:
			old.ambiguous = true
			fields[name] = old
		}
	}
	return func(key string) reflect.Type {
		if f, ok := fields[key]; ok && !f.ambiguous {
			return f.Type
		}
		var best field
		for name, f := range fields {
			if f.ambiguous || !f.fold || !strings.EqualFold(name, key) {
				continue
			}
			if best.Type == nil || len(f.Index) < len(best.Index) || len(f.Index) == len(best.Index) && slices.Compare(f.Index, best.Index) < 0 {
				best = f
			}
		}
		if best.Type != nil {
			return best.Type
		}
		return inline
	}
}

// Accept typed maps and YAML's interface-keyed maps without silently dropping
// keys that cannot be represented by the KeyTree string-key contract.
func strictConfigObject(raw any, path string) (map[string]any, error) {
	if object, ok := raw.(map[string]any); ok {
		return object, nil
	}
	value := reflect.ValueOf(raw)
	if value.Kind() != reflect.Map {
		return nil, nil
	}
	object := make(map[string]any, value.Len())
	iter := value.MapRange()
	for iter.Next() {
		key := iter.Key()
		if key.Kind() == reflect.Interface && !key.IsNil() {
			key = key.Elem()
		}
		if key.Kind() != reflect.String {
			return nil, fmt.Errorf("cannot inspect config field %q with RejectUnknown: KeyTree object keys must be strings", path)
		}
		object[key.String()] = iter.Value().Interface()
	}
	return object, nil
}

func configContainsObjects(t reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[t] {
		return false
	}
	visiting[t] = true
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return configContainsObjects(t.Elem(), visiting)
	}
	return false
}

func configMemberPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

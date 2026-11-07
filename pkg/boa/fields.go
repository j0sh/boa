package boa

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ValidationCode identifies a rule independently of its human-readable message.
type ValidationCode string

const (
	CodeRequired     ValidationCode = "required"
	CodeAlternatives ValidationCode = "alternatives"
	CodeMin          ValidationCode = "min"
	CodeMax          ValidationCode = "max"
	CodePattern      ValidationCode = "pattern"
	CodeCustom       ValidationCode = "custom"
	CodeSchema       ValidationCode = "schema"
	CodeLimit        ValidationCode = "limit"
)

// MaxValidationErrors bounds the errors returned by Validate.
const MaxValidationErrors = 100

// FieldError describes a failed rule. Path uses exported Go field names, dots,
// slice/array indices, and quoted string map keys, e.g. Apps[0].Labels["team"].
// Err is the underlying diagnostic; custom validator errors support errors.Is.
type FieldError struct {
	Path string
	Code ValidationCode
	Err  error
}

func (e *FieldError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return e.Path + ": " + e.Err.Error()
}

func (e *FieldError) Unwrap() error { return e.Err }

// ValidationError contains at most MaxValidationErrors failures, in traversal
// order, with at most one rule failure per field. Truncated means traversal
// stopped at the error cap; further fields may have failures.
type ValidationError struct {
	Fields    []FieldError
	Truncated bool
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}
	return fmt.Sprintf("validation failed (%d errors): %s", len(e.Fields), e.Fields[0].Error())
}

func (e *ValidationError) Unwrap() []error {
	errs := make([]error, len(e.Fields))
	for i := range e.Fields {
		errs[i] = &e.Fields[i]
	}
	return errs
}

// ConfigureField edits existing Boa metadata for an in-memory operation. It is
// called after tags, using the same paths as FieldError. It never runs CLI hooks.
// Use Parameter.SetCustomValidator or Field[T]{Parameter: p} for typed validators.
// Callbacks and validators are application code and should be free of I/O.
type ConfigureField func(path string, field Parameter)

// Defaults constructs a fresh struct using default tags and optional metadata
// configuration. It does not validate, read the environment, resolve paths, read
// files or secrets, or run lifecycle hooks. Nil struct groups stay nil. Collection
// defaults are whole values; element defaults are not overlaid onto them.
// Decode into the returned value to preserve explicit zero/false/empty inputs.
func Defaults[T any](configure ...ConfigureField) (T, error) {
	var value T
	root := reflect.ValueOf(&value).Elem()
	if root.Kind() != reflect.Struct {
		return value, fmt.Errorf("boa.Defaults: expected a struct type")
	}
	err := walkFields(root, "", make(map[configCopyKey]bool), 0, func(field reflect.StructField, v reflect.Value, path string) (bool, error) {
		meta, err := fieldMetadata(field, path, true, configure)
		if err != nil {
			return false, &FieldError{Path: path, Code: CodeSchema, Err: err}
		}
		if meta.IsIgnored() || !meta.IsEnabled() {
			return false, nil
		}
		if meta.hasDefaultValue() {
			value := reflect.ValueOf(meta.defaultValuePtr()).Elem()
			if meta.isPointer {
				v.Set(reflect.New(v.Type().Elem()))
				v = v.Elem()
			}
			// Mirrors normalize named primitive/slice types for Cobra. Use the
			// same typed view as syncMirrors when constructing their defaults.
			reinterpretAs(v, value.Type()).Set(value)
			return false, nil
		}
		return true, nil
	}, nil)
	return value, err
}

// Validate checks an in-memory struct or non-nil pointer to one, without applying
// defaults or mutating input. Scalars (including 0, false, and "") are present;
// nil pointers, maps, and slices are absent. Use min/pattern/custom rules to reject
// zero or empty scalar values. Absent optional values skip rules and traversal.
//
// Nested structs and collection elements are checked. Registered/text scalar
// types are opaque. Cyclic references are visited once per active branch, and
// nesting beyond 128 levels returns CodeLimit. No environment, filesystem, path,
// secret, command HTTPBind policy, or lifecycle operations run. Custom validators
// receive a deep copy; their own external side effects remain their responsibility.
func Validate(value any, configure ...ConfigureField) error {
	root := reflect.ValueOf(value)
	if root.IsValid() && root.Kind() == reflect.Pointer && !root.IsNil() {
		root = root.Elem()
	}
	errs := &ValidationError{}
	add := func(path string, code ValidationCode, err error) {
		errs.Fields = append(errs.Fields, FieldError{Path: path, Code: code, Err: err})
		errs.Truncated = len(errs.Fields) == MaxValidationErrors
	}
	if !root.IsValid() || root.Kind() != reflect.Struct {
		add("", CodeSchema, fmt.Errorf("boa.Validate: expected a struct or non-nil pointer to struct"))
		return errs
	}
	root = cloneConfigValue(root, make(map[configCopyKey]reflect.Value))
	err := walkFields(root, "", make(map[configCopyKey]bool), 0, func(field reflect.StructField, v reflect.Value, path string) (bool, error) {
		meta, err := fieldMetadata(field, path, false, configure)
		if err != nil {
			add(path, CodeSchema, err)
			return false, nil
		}
		if meta.IsIgnored() || !meta.IsEnabled() {
			return false, nil
		}
		present := true
		switch v.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			present = !v.IsNil()
		}
		if err := validateRequired(meta, present); err != nil {
			add(path, CodeRequired, err.(*FieldError).Err)
			return false, nil
		}
		if !present {
			return false, nil
		}
		ptr := v.Addr().Interface()
		if meta.isPointer {
			ptr = v.Interface()
		}
		for _, rule := range []func() error{
			func() error { return validateAlternatives(meta, ptr) },
			func() error { return validateCustom(meta, ptr) },
			func() error { return validateMinMaxPattern(meta, ptr) },
		} {
			if err := rule(); err != nil {
				failure := err.(*FieldError)
				add(path+failure.Path, failure.Code, failure.Err)
				return false, nil
			}
		}
		return true, nil
	}, func() bool { return errs.Truncated })
	if err != nil {
		failure := err.(*FieldError)
		add(failure.Path, failure.Code, failure.Err)
	}
	if len(errs.Fields) != 0 {
		return errs
	}
	return nil
}

func fieldMetadata(field reflect.StructField, path string, defaults bool, configure []ConfigureField) (*paramMeta, error) {
	param, err := newParam(&field, field.Type)
	if err != nil {
		return nil, err
	}
	meta := param.(*paramMeta)
	meta.name = path
	if err := applyFieldTags(meta, field.Tag, defaults); err != nil {
		return nil, err
	}
	for _, fn := range configure {
		fn(path, meta)
	}
	return meta, nil
}

// walkFields only traverses values. Source handling and CLI mirror identity stay
// in traverseAt; this walker also handles structs inside collections.
func walkFields(v reflect.Value, path string, active map[configCopyKey]bool, depth int, visit func(reflect.StructField, reflect.Value, string) (bool, error), stop func() bool) error {
	if stop != nil && stop() {
		return nil
	}
	if depth > 128 {
		return &FieldError{Path: path, Code: CodeLimit, Err: fmt.Errorf("nesting exceeds 128 levels")}
	}
	if lookupHandler(v.Type()) != nil {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return nil
		}
		key := configCopyKey{typ: v.Type(), ptr: v.UnsafePointer()}
		if v.Kind() == reflect.Slice {
			key.len = v.Len()
		}
		if active[key] {
			return nil
		}
		active[key] = true
		defer delete(active, key)
	}
	walk := func(child reflect.Value, childPath string) error {
		return walkFields(child, childPath, active, depth+1, visit, stop)
	}
	switch v.Kind() {
	case reflect.Pointer:
		return walk(v.Elem(), path)
	case reflect.Interface:
		if !v.IsNil() {
			child := reflect.New(v.Elem().Type()).Elem()
			child.Set(v.Elem())
			return walk(child, path)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if stop != nil && stop() {
				break
			}
			field := v.Type().Field(i)
			if isBoaIgnored(field) {
				continue
			}
			childPath := field.Name
			if path != "" {
				childPath = path + "." + childPath
			}
			child := v.Field(i)
			recurse, err := visit(field, child, childPath)
			if err != nil {
				return err
			}
			if recurse {
				if err := walk(child, childPath); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if stop != nil && stop() {
				break
			}
			if err := walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return &FieldError{Path: path, Code: CodeSchema, Err: fmt.Errorf("expected string map keys")}
		}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
		for _, key := range keys {
			if stop != nil && stop() {
				break
			}
			// Map values are not addressable, so visit an addressable copy.
			child := reflect.New(v.Type().Elem()).Elem()
			child.Set(v.MapIndex(key))
			if err := walk(child, path+"["+strconv.Quote(key.String())+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

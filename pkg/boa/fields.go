package boa

import (
	"fmt"
	"reflect"
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

const maxFieldDepth = 128
const maxFieldSteps = 10_000

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
	walker := fieldWalker{active: make(map[configCopyKey]bool), remaining: maxFieldSteps}
	err := walker.walk(root, "", 0, func(field reflect.StructField, v reflect.Value, path string) (bool, error) {
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
	})
	return value, err
}

// Validate checks an in-memory struct or non-nil pointer to one, without applying
// defaults or mutating input. Scalars (including 0, false, and "") are present;
// nil pointers, maps, and slices are absent. Use min/pattern/custom rules to reject
// zero or empty scalar values. Absent optional values skip rules and traversal.
//
// Nested structs and collection elements are checked. Registered/text scalar
// types are opaque. Cycles are visited once per active branch. Traversal is
// limited to 10,000 steps and 128 recursive steps, including pointer/interface
// dereferences. Limit failures return CodeLimit. No environment, filesystem, path,
// secret, command HTTPBind policy, or lifecycle operations run. Custom validators
// receive a deep copy; their own external side effects remain their responsibility.
func Validate(value any, configure ...ConfigureField) error {
	root := reflect.ValueOf(value)
	active := make(map[configCopyKey]bool)
	if root.IsValid() && root.Kind() == reflect.Pointer && !root.IsNil() {
		active[configCopyKey{typ: root.Type(), ptr: root.UnsafePointer()}] = true
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
	// Keep traversal addressable without copying excluded subtrees. Only rules
	// that can invoke application code need an independent field copy.
	addressable := reflect.New(root.Type()).Elem()
	addressable.Set(root)
	root = addressable
	walker := fieldWalker{active: active, remaining: maxFieldSteps,
		stopped: func() bool { return errs.Truncated },
		report:  func(failure *FieldError) { add(failure.Path, failure.Code, failure.Err) },
	}
	_ = walker.walk(root, "", 0, func(field reflect.StructField, v reflect.Value, path string) (bool, error) {
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
		ruleValue := v
		if meta.customValidator != nil || meta.GetAlternatives() != nil && meta.GetStrictAlts() {
			cloner := configCloner{seen: make(map[configCopyKey]reflect.Value), maxDepth: maxFieldDepth}
			ruleValue = cloner.clone(v, 0)
			if cloner.exceeded {
				add(path, CodeLimit, fmt.Errorf("copying exceeds %d recursive steps", maxFieldDepth))
				return false, nil
			}
		}
		ptr := ruleValue.Addr().Interface()
		if meta.isPointer {
			ptr = ruleValue.Interface()
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
	})
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

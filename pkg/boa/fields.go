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

// MaxValidationErrors bounds shared validation diagnostics.
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
	limit     int
	message   string
}

func (e *ValidationError) append(path string, failure *FieldError) {
	if !e.Truncated {
		e.Fields = append(e.Fields, FieldError{Path: path + failure.Path, Code: failure.Code, Err: failure.Err})
		e.Truncated = len(e.Fields) == e.limit
	}
}

func (e *ValidationError) Error() string {
	if e.message != "" {
		return e.message
	}
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
// called after tags, using the same paths as FieldError. Explicit settings
// override the tag baseline, as they do in CLI Init hooks.
// It never runs CLI hooks.
// Use Parameter.SetCustomValidator or Field[T]{Parameter: p} for typed validators.
// Callbacks and validators are application code and should be free of I/O.
type ConfigureField func(path string, field Parameter)

// Defaults constructs a fresh struct using default tags and optional metadata
// configuration. It does not validate, read the environment, resolve paths, read
// files or secrets, or run lifecycle hooks. Nil struct groups stay nil. Collection
// defaults are whole values; element defaults are not overlaid onto them.
// Decoded zero, false and empty strings replace defaults. Collection merging
// and replacement follow the supplied decoder.
func Defaults[T any](configure ...ConfigureField) (T, error) {
	var value T
	if failures := processFields(&value, true, nil, configure); failures != nil {
		return value, &failures.Fields[0]
	}
	return value, nil
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
	if failures := processFields(value, false, nil, configure); failures != nil {
		return failures
	}
	return nil
}

// processFields owns metadata, default construction or validation, traversal,
// and diagnostics for both public APIs and CLI values. CLI supplies its existing
// metadata/source state; it does not supply a second validation controller.
func processFields(value any, defaults bool, ctx *processingContext, configure []ConfigureField) *ValidationError {
	root := reflect.ValueOf(value)
	active := make(map[configCopyKey]bool)
	if root.IsValid() && root.Kind() == reflect.Pointer && !root.IsNil() {
		active[configCopyKey{typ: root.Type(), ptr: root.UnsafePointer()}] = true
		root = root.Elem()
	}
	failures := &ValidationError{limit: MaxValidationErrors}
	if defaults {
		failures.limit = 1
	}
	if !root.IsValid() || root.Kind() != reflect.Struct {
		failures.append("", ruleError(CodeSchema, fmt.Errorf("boa: expected a struct or non-nil pointer to struct")))
		return failures
	}
	if !root.CanAddr() {
		addressable := reflect.New(root.Type()).Elem()
		addressable.Set(root)
		root = addressable
	}
	walker := fieldWalker{active: active, remaining: maxFieldSteps, failures: failures}
	_ = walker.walk(root, "", 0, func(field reflect.StructField, v reflect.Value, path string) (bool, error) {
		var meta *paramMeta
		cached := false
		if ctx != nil {
			meta, cached = ctx.mirrorByPath[joinPath(walker.indices)].(*paramMeta)
		}
		if !cached {
			var failure *FieldError
			meta, failure = newField(field, path)
			if failure != nil {
				failures.append("", failure)
				return false, nil
			}
			if defaults {
				if err := applyDefaultTag(meta, field.Tag); err != nil {
					failures.append("", schemaError(path, field.Tag, err))
					return false, nil
				}
			}
			for _, fn := range configure {
				fn(path, meta)
			}
		}
		if meta.IsIgnored() || !meta.IsEnabled() {
			return false, nil
		}
		if defaults {
			if meta.hasDefaultValue() {
				syncField(v, meta)
				return false, nil
			}
			return v.Kind() != reflect.Array && mayHaveFields(field.Type), nil
		}
		valuePresent := true
		switch v.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			valuePresent = !v.IsNil()
		}
		present := valuePresent
		if cached {
			present = meta.HasValue()
		}
		if failure := validateRequired(meta, present); failure != nil {
			failures.append(path, failure)
			return false, nil
		}
		if !present || !valuePresent {
			return false, nil
		}
		original := v.Addr().Interface()
		if meta.isPointer {
			original = v.Interface()
		}
		ruleValue := v
		if meta.customValidator != nil || meta.GetAlternatives() != nil && meta.GetStrictAlts() {
			cloner := configCloner{seen: make(map[configCopyKey]reflect.Value), maxDepth: maxFieldDepth}
			ruleValue = cloner.clone(v, 0)
			if cloner.exceeded {
				failures.append(path, ruleError(CodeLimit, fmt.Errorf("copying exceeds %d recursive steps", maxFieldDepth)))
				return false, nil
			}
		}
		ptr := ruleValue.Addr().Interface()
		if meta.isPointer {
			ptr = ruleValue.Interface()
		}
		failure := validateAlternatives(meta, ptr)
		if failure == nil {
			failure = validateCustom(meta, ptr)
		}
		if failure == nil {
			failure = validateMinMaxPattern(meta, original)
		}
		if failure != nil {
			failures.append(path, failure)
			return false, nil
		}
		return mayHaveFields(field.Type), nil
	})
	if len(failures.Fields) == 0 {
		return nil
	}
	return failures
}

func schemaError(path string, tags reflect.StructTag, err error) *FieldError {
	if alias := tags.Get("name"); alias != "" {
		err = fmt.Errorf("(%s) %w", alias, err)
	}
	return &FieldError{Path: path, Code: CodeSchema, Err: err}
}

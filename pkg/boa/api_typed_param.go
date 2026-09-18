// Package boa provides a declarative CLI and environment variable parameter utility.
package boa

import (
	"fmt"
	"log/slog"
	"reflect"
)

// Field is a type-safe view of one field in a command's parameter struct.
// Obtain it with Param; the embedded Parameter exposes metadata
// methods that do not depend on the field's Go type.
type Field[T any] struct{ Parameter }

// Param returns the type-safe view for fieldPtr. It returns nil, after logging
// the reason, when fieldPtr is not part of this command's parameter struct.
func Param[T any](ctx *HookContext, fieldPtr *T) *Field[T] {
	p := ctx.parameter(fieldPtr)
	if p == nil {
		return nil
	}
	return &Field[T]{Parameter: p}
}

// SetDefault sets the default value with compile-time type checking.
func (p *Field[T]) SetDefault(value T) {
	p.Parameter.SetDefault(value)
}

// SetCustomValidator sets a type-safe validation function.
func (p *Field[T]) SetCustomValidator(fn func(T) error) {
	if fn == nil {
		p.Parameter.SetCustomValidator(nil)
		return
	}
	p.Parameter.SetCustomValidator(func(value any) error {
		switch v := value.(type) {
		case T:
			return fn(v)
		case *T:
			if v != nil {
				return fn(*v)
			}
			var zero T
			return fn(zero)
		default:
			// Parameter mirrors use normalized primitive types. Convert those
			// values back to named field types before invoking the validator.
			rv := reflect.ValueOf(value)
			if rv.Kind() == reflect.Pointer && !rv.IsNil() {
				rv = rv.Elem()
			}
			target := reflect.TypeFor[T]()
			if rv.IsValid() && rv.Type().ConvertibleTo(target) {
				return fn(rv.Convert(target).Interface().(T))
			}
			slog.Warn("boa.Field.SetCustomValidator: unexpected value type", "expected", target, "got", reflect.TypeOf(value))
			return fn(value.(T))
		}
	})
}

// SetMin sets a typed numeric lower bound. Use SetMinLen for strings, slices,
// and maps.
func (p *Field[T]) SetMin(min T) {
	assertNumericT[T]("SetMin")
	p.Parameter.SetMin(min)
}

// SetMax sets a typed numeric upper bound. Use SetMaxLen for strings, slices,
// and maps.
func (p *Field[T]) SetMax(max T) {
	assertNumericT[T]("SetMax")
	p.Parameter.SetMax(max)
}

// SetMinLen sets a minimum length for a string, slice, or map.
func (p *Field[T]) SetMinLen(min int) {
	assertLengthT[T]("SetMinLen")
	p.Parameter.SetMin(min)
}

// SetMaxLen sets a maximum length for a string, slice, or map.
func (p *Field[T]) SetMaxLen(max int) {
	assertLengthT[T]("SetMaxLen")
	p.Parameter.SetMax(max)
}

func assertNumericT[T any](method string) {
	kind := reflect.TypeFor[T]().Kind()
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return
	}
	panic(fmt.Errorf("boa: %s requires a numeric field, got %s; use SetMinLen or SetMaxLen for strings, slices, and maps", method, kind))
}

func assertLengthT[T any](method string) {
	kind := reflect.TypeFor[T]().Kind()
	switch kind {
	case reflect.String, reflect.Slice, reflect.Map:
		return
	}
	panic(fmt.Errorf("boa: %s requires a string, slice, or map field, got %s; use SetMin or SetMax for numeric fields", method, kind))
}

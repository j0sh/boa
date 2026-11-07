package boa

import (
	"cmp"
	"fmt"
	"reflect"
	"regexp"
	"slices"
)

func ruleError(code ValidationCode, err error) *FieldError {
	return &FieldError{Code: code, Err: err}
}

func validateRequired(param parameter, present bool) *FieldError {
	if param.IsRequired() && !present {
		return ruleError(CodeRequired, fmt.Errorf("required value is absent"))
	}
	return nil
}

func validateAlternatives(param parameter, valPtr any) *FieldError {
	alts := param.GetAlternatives()
	if alts == nil || !param.GetStrictAlts() || valPtr == nil {
		return nil
	}
	// CLI mirrors normalize named primitives, including slice elements. Use
	// that same representation rather than a named type's display methods.
	value := reinterpretAs(reflect.ValueOf(valPtr).Elem(), param.GetType())
	check := func(v reflect.Value, path string) *FieldError {
		text := fmt.Sprintf("%v", v.Interface())
		if !slices.Contains(alts, text) {
			err := ruleError(CodeAlternatives, fmt.Errorf("'%s' is not in the list of allowed values: %v", text, alts))
			err.Path = path
			return err
		}
		return nil
	}
	if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			if err := check(value.Index(i), fmt.Sprintf("[%d]", i)); err != nil {
				return err
			}
		}
		return nil
	}
	return check(value, "")
}

func validateCustom(param parameter, valPtr any) *FieldError {
	if valPtr != nil {
		value := reinterpretAs(reflect.ValueOf(valPtr).Elem(), param.GetType())
		valPtr = value.Addr().Interface()
	}
	if err := param.customValidatorOfPtr()(valPtr); err != nil {
		return ruleError(CodeCustom, err)
	}
	return nil
}

// validateMinMaxPattern checks min/max/pattern tag constraints. It dispatches
// on the field's boundKind and compares against the typed-pointer bound stored
// in pm.minVal / pm.maxVal. The storage type is guaranteed to match the field
// kind (coerceBound enforces this at set time), so the type assertions here
// never panic on a valid mirror.
func validateMinMaxPattern(pm *paramMeta, valPtr any) *FieldError {
	if pm.minVal == nil && pm.maxVal == nil && pm.pattern == "" {
		return nil
	}

	v := reflect.ValueOf(valPtr)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}

	var failure *FieldError
	switch pm.boundKind() {
	case signedIntBound:
		failure = validateBounds(v.Int(), pm.minVal, pm.maxVal, "value")
	case unsignedIntBound:
		failure = validateBounds(v.Uint(), pm.minVal, pm.maxVal, "value")
	case floatBound:
		failure = validateBounds(v.Float(), pm.minVal, pm.maxVal, "value")
	case lengthBound:
		failure = validateBounds(v.Len(), pm.minVal, pm.maxVal, "length")
	}
	if failure != nil {
		return failure
	}

	if pm.pattern != "" && v.Kind() == reflect.String {
		matched, err := regexp.MatchString(pm.pattern, v.String())
		if err != nil {
			return ruleError(CodePattern, fmt.Errorf("invalid pattern %q: %w", pm.pattern, err))
		}
		if !matched {
			return ruleError(CodePattern, fmt.Errorf("value %q does not match pattern %q", v.String(), pm.pattern))
		}
	}

	return nil
}

func validateBounds[T cmp.Ordered](value T, min, max any, label string) *FieldError {
	if bound, ok := min.(*T); ok && bound != nil && value < *bound {
		return ruleError(CodeMin, fmt.Errorf("%s %v is below min %v", label, value, *bound))
	}
	if bound, ok := max.(*T); ok && bound != nil && value > *bound {
		return ruleError(CodeMax, fmt.Errorf("%s %v exceeds max %v", label, value, *bound))
	}
	return nil
}

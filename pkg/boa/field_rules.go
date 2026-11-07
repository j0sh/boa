package boa

import (
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

	switch pm.boundKind() {
	case signedIntBound:
		val := v.Int()
		if minP, ok := pm.minVal.(*int64); ok && minP != nil && val < *minP {
			return ruleError(CodeMin, fmt.Errorf("value %d is below min %d", val, *minP))
		}
		if maxP, ok := pm.maxVal.(*int64); ok && maxP != nil && val > *maxP {
			return ruleError(CodeMax, fmt.Errorf("value %d exceeds max %d", val, *maxP))
		}
	case unsignedIntBound:
		val := v.Uint()
		if minP, ok := pm.minVal.(*uint64); ok && minP != nil && val < *minP {
			return ruleError(CodeMin, fmt.Errorf("value %d is below min %d", val, *minP))
		}
		if maxP, ok := pm.maxVal.(*uint64); ok && maxP != nil && val > *maxP {
			return ruleError(CodeMax, fmt.Errorf("value %d exceeds max %d", val, *maxP))
		}
	case floatBound:
		val := v.Float()
		if minP, ok := pm.minVal.(*float64); ok && minP != nil && val < *minP {
			return ruleError(CodeMin, fmt.Errorf("value %v is below min %v", val, *minP))
		}
		if maxP, ok := pm.maxVal.(*float64); ok && maxP != nil && val > *maxP {
			return ruleError(CodeMax, fmt.Errorf("value %v exceeds max %v", val, *maxP))
		}
	case lengthBound:
		var l int
		switch v.Kind() {
		case reflect.String:
			l = len(v.String())
		case reflect.Slice, reflect.Map:
			l = v.Len()
		}
		if minP, ok := pm.minVal.(*int); ok && minP != nil && l < *minP {
			return ruleError(CodeMin, fmt.Errorf("length %d is below min %d", l, *minP))
		}
		if maxP, ok := pm.maxVal.(*int); ok && maxP != nil && l > *maxP {
			return ruleError(CodeMax, fmt.Errorf("length %d exceeds max %d", l, *maxP))
		}
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

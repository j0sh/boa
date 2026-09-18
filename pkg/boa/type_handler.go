package boa

import (
	"encoding"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// typeHandler defines how a specific Go type is handled as a CLI parameter.
// Each handler covers the full lifecycle: flag binding, string parsing, and post-parse conversion.
type typeHandler struct {
	// bindFlag registers a cobra flag and returns the pointer cobra writes to.
	// name, short, descr are the flag metadata. defaultVal is from paramMeta.defaultValuePtr() (may be nil).
	bindFlag func(cmd *cobra.Command, name, short, descr string, defaultVal any) any

	// parse converts a string value (from env var, default tag, positional arg) into a typed pointer.
	// Returns *T for the appropriate type.
	parse func(name, strVal string) (any, error)

	// convert is called during validation to convert cobra's stored type to the final type.
	// Only needed for types stored as strings in cobra (time.Time, *url.URL).
	// nil means no conversion needed.
	convert func(name string, val any) (any, error)

	// baseType is the canonical Go type, used by normalizeType().
	// e.g., for time.Duration this is reflect.TypeOf(time.Duration(0))
	baseType reflect.Type
	format   func(reflect.Value) string
}

// typeHandlerRegistry maps reflect.Type → handler for special types (time.Time, net.IP, etc.)
// and reflect.Kind → handler for basic types (string, int, bool, etc.)
var (
	exactTypeHandlers = map[reflect.Type]*typeHandler{}
	kindHandlers      = map[reflect.Kind]*typeHandler{}

	// sliceTypeHandlers maps the element type to a handler for slices of that type.
	// Used for []time.Duration, []time.Time, []net.IP, []*url.URL.
	sliceExactTypeHandlers = map[reflect.Type]*typeHandler{}
	sliceKindHandlers      = map[reflect.Kind]*typeHandler{}
)

func init() {
	registerBuiltinTypes()
}

// TypeDef defines how a custom type is parsed from and formatted to strings.
// Use with RegisterType to add support for user-defined types as CLI parameters.
type TypeDef[T any] struct {
	// Parse converts a CLI string into the typed value.
	Parse func(string) (T, error)
	// Format converts the typed value back to a string (for default display).
	// If nil, fmt.Sprintf("%v", val) is used.
	Format func(T) string
}

// RegisterType registers a custom type for use as a CLI parameter.
// The type will be stored as a string flag in cobra and converted using
// the provided Parse/Format functions. Call RegisterType during setup, before
// constructing commands or decoding config concurrently.
//
// Example:
//
//	boa.RegisterType[SemVer](boa.TypeDef[SemVer]{
//	    Parse:  func(s string) (SemVer, error) { return parseSemVer(s) },
//	    Format: func(v SemVer) string { return v.String() },
//	})
func RegisterType[T any](def TypeDef[T]) {
	if def.Parse == nil {
		panic("boa: TypeDef.Parse must not be nil")
	}
	exactTypeHandlers[reflect.TypeFor[T]()] = stringHandler(def)
}

func stringHandler[T any](def TypeDef[T]) *typeHandler {
	return parsedStringHandler(reflect.TypeFor[T](), func(text string) (reflect.Value, error) {
		value, err := def.Parse(text)
		return reflect.ValueOf(&value).Elem(), err
	}, func(value reflect.Value) string {
		if def.Format != nil {
			return def.Format(value.Interface().(T))
		}
		return fmt.Sprint(value.Interface())
	})
}

func parsedStringHandler(t reflect.Type, parse func(string) (reflect.Value, error), format func(reflect.Value) string) *typeHandler {
	h := &typeHandler{baseType: t, format: format}
	h.parse = func(name, text string) (any, error) {
		value, err := parse(text)
		if err != nil {
			return nil, fmt.Errorf("invalid value for param %s: %w", name, err)
		}
		result := reflect.New(t)
		result.Elem().Set(value)
		return result.Interface(), nil
	}
	h.bindFlag = func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
		var text string
		if defaultVal != nil {
			text = format(reflect.ValueOf(defaultVal).Elem())
		}
		return cmd.Flags().StringP(name, short, text, descr)
	}
	h.convert = func(name string, value any) (any, error) {
		if text, ok := value.(*string); ok {
			return h.parse(name, *text)
		}
		return value, nil
	}
	return h
}

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

func textHandler(t reflect.Type) *typeHandler {
	if t.Kind() == reflect.Pointer || !reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return nil
	}
	return parsedStringHandler(t, func(text string) (reflect.Value, error) {
		value := reflect.New(t)
		err := value.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(text))
		return value.Elem(), err
	}, func(value reflect.Value) string {
		if marshaler, ok := value.Addr().Interface().(encoding.TextMarshaler); ok {
			if text, err := marshaler.MarshalText(); err == nil {
				return string(text)
			}
		}
		return fmt.Sprint(value.Interface())
	})
}

func registerBuiltinTypes() {
	kindHandlers = map[reflect.Kind]*typeHandler{
		reflect.String:  nativeHandler((*pflag.FlagSet).StringP, parseString),
		reflect.Bool:    nativeHandler((*pflag.FlagSet).BoolP, strconv.ParseBool),
		reflect.Int:     nativeHandler((*pflag.FlagSet).IntP, parseSigned[int]),
		reflect.Int8:    nativeHandler((*pflag.FlagSet).Int8P, parseSigned[int8]),
		reflect.Int16:   nativeHandler((*pflag.FlagSet).Int16P, parseSigned[int16]),
		reflect.Int32:   nativeHandler((*pflag.FlagSet).Int32P, parseSigned[int32]),
		reflect.Int64:   nativeHandler((*pflag.FlagSet).Int64P, parseSigned[int64]),
		reflect.Uint:    nativeHandler((*pflag.FlagSet).UintP, parseUnsigned[uint]),
		reflect.Uint8:   nativeHandler((*pflag.FlagSet).Uint8P, parseUnsigned[uint8]),
		reflect.Uint16:  nativeHandler((*pflag.FlagSet).Uint16P, parseUnsigned[uint16]),
		reflect.Uint32:  nativeHandler((*pflag.FlagSet).Uint32P, parseUnsigned[uint32]),
		reflect.Uint64:  nativeHandler((*pflag.FlagSet).Uint64P, parseUnsigned[uint64]),
		reflect.Float32: nativeHandler((*pflag.FlagSet).Float32P, parseFloat[float32]),
		reflect.Float64: nativeHandler((*pflag.FlagSet).Float64P, parseFloat[float64]),
	}
	// --- Special types (by exact type) ---

	exactTypeHandlers[durationType] = nativeHandler((*pflag.FlagSet).DurationP, time.ParseDuration)
	exactTypeHandlers[ipType] = nativeHandler((*pflag.FlagSet).IPP, parseIP)
	exactTypeHandlers[timeType] = stringHandler(TypeDef[time.Time]{Parse: parseTimeString, Format: func(t time.Time) string { return t.Format(time.RFC3339) }})
	exactTypeHandlers[urlPtrType] = stringHandler(TypeDef[*url.URL]{Parse: func(text string) (*url.URL, error) {
		if text == "" {
			return nil, nil
		}
		return url.Parse(text)
	}, Format: func(value *url.URL) string {
		if value == nil {
			return ""
		}
		return value.String()
	}})

	sliceExactTypeHandlers[ipType] = nativeSliceHandler((*pflag.FlagSet).IPSliceP, parseIP)
	sliceExactTypeHandlers[durationType] = nativeSliceHandler((*pflag.FlagSet).DurationSliceP, time.ParseDuration)

	sliceKindHandlers = map[reflect.Kind]*typeHandler{
		reflect.String:  nativeSliceHandler((*pflag.FlagSet).StringSliceP, parseString),
		reflect.Bool:    nativeSliceHandler((*pflag.FlagSet).BoolSliceP, strconv.ParseBool),
		reflect.Int:     nativeSliceHandler((*pflag.FlagSet).IntSliceP, parseSigned[int]),
		reflect.Int32:   nativeSliceHandler((*pflag.FlagSet).Int32SliceP, parseSigned[int32]),
		reflect.Int64:   nativeSliceHandler((*pflag.FlagSet).Int64SliceP, parseSigned[int64]),
		reflect.Uint:    nativeSliceHandler((*pflag.FlagSet).UintSliceP, parseUnsigned[uint]),
		reflect.Float32: nativeSliceHandler((*pflag.FlagSet).Float32SliceP, parseFloat[float32]),
		reflect.Float64: nativeSliceHandler((*pflag.FlagSet).Float64SliceP, parseFloat[float64]),
		reflect.Int8:    makeIntSliceFallbackHandler(reflect.TypeFor[int8](), "int8Slice", parseSigned[int8], formatScalar[int8]),
		reflect.Int16:   makeIntSliceFallbackHandler(reflect.TypeFor[int16](), "int16Slice", parseSigned[int16], formatScalar[int16]),
		reflect.Uint8:   makeIntSliceFallbackHandler(reflect.TypeFor[uint8](), "uint8Slice", parseUnsigned[uint8], formatScalar[uint8]),
		reflect.Uint16:  makeIntSliceFallbackHandler(reflect.TypeFor[uint16](), "uint16Slice", parseUnsigned[uint16], formatScalar[uint16]),
		reflect.Uint32:  makeIntSliceFallbackHandler(reflect.TypeFor[uint32](), "uint32Slice", parseUnsigned[uint32], formatScalar[uint32]),
		reflect.Uint64:  makeIntSliceFallbackHandler(reflect.TypeFor[uint64](), "uint64Slice", parseUnsigned[uint64], formatScalar[uint64]),
	}
}

// nativeHandler retains pflag's native value types while sharing default and
// string conversion. Named Go scalar types use the same underlying parser.
func nativeHandler[T any](bind func(*pflag.FlagSet, string, string, T, string) *T, parse func(string) (T, error)) *typeHandler {
	return &typeHandler{
		baseType: reflect.TypeFor[T](),
		bindFlag: func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			var def T
			if defaultVal != nil {
				def = reflect.ValueOf(defaultVal).Elem().Convert(reflect.TypeFor[T]()).Interface().(T)
			}
			return bind(cmd.Flags(), name, short, def, descr)
		},
		parse: func(name, text string) (any, error) {
			value, err := parse(text)
			if err != nil {
				return nil, fmt.Errorf("invalid value for param %s: %w", name, err)
			}
			return new(value), nil
		},
	}
}

func nativeSliceHandler[T any](bind func(*pflag.FlagSet, string, string, []T, string) *[]T, parse func(string) (T, error)) *typeHandler {
	return &typeHandler{
		baseType: reflect.TypeFor[[]T](),
		bindFlag: func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			return bind(cmd.Flags(), name, short, toTypedSlice[T](derefSliceDefault(defaultVal)), descr)
		},
		parse: func(name, text string) (any, error) { return parseSliceWith(text, parse) },
	}
}

func parseString(s string) (string, error) { return s, nil }
func formatScalar[T any](v T) string       { return fmt.Sprint(v) }
func parseSigned[T ~int | ~int8 | ~int16 | ~int32 | ~int64](s string) (T, error) {
	v, err := strconv.ParseInt(s, 10, reflect.TypeFor[T]().Bits())
	return T(v), err
}
func parseUnsigned[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](s string) (T, error) {
	v, err := strconv.ParseUint(s, 10, reflect.TypeFor[T]().Bits())
	return T(v), err
}
func parseFloat[T ~float32 | ~float64](s string) (T, error) {
	v, err := strconv.ParseFloat(s, reflect.TypeFor[T]().Bits())
	return T(v), err
}

func parseIP(text string) (net.IP, error) {
	ip := net.ParseIP(text)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", text)
	}
	return ip, nil
}

// lookupHandler resolves scalar types; registered exact types take precedence.
func lookupHandler(t reflect.Type) *typeHandler {
	if h := exactTypeHandlers[t]; h != nil {
		return h
	}
	if h := textHandler(t); h != nil {
		return h
	}
	return kindHandlers[t.Kind()]
}

// resolveHandler is the shared dispatch for flags, env, defaults, and validation.
func resolveHandler(t reflect.Type) *typeHandler {
	if h := lookupHandler(t); h != nil {
		return h
	}
	switch t.Kind() {
	case reflect.Map:
		if h := lookupMapHandler(t); h != nil {
			return h
		}
		return jsonFallbackHandler(t)
	case reflect.Slice:
		if h := lookupSliceHandler(t.Elem()); h != nil {
			return h
		}
		return jsonFallbackHandler(t)
	}
	return nil
}

// lookupSliceHandler finds the handler for a slice type based on its element type.
func lookupSliceHandler(elemType reflect.Type) *typeHandler {
	// Exact element type match first
	if h, ok := sliceExactTypeHandlers[elemType]; ok {
		return h
	}
	if h := lookupHandler(elemType); h != nil && h.format != nil {
		return parsedSliceHandler(h)
	}
	// Kind-based match
	if h, ok := sliceKindHandlers[elemType.Kind()]; ok {
		return h
	}
	return nil
}

// parsedSliceHandler shares one element codec for custom scalars, text values,
// time.Time, and URLs. pflag owns CSV/repeated-flag semantics.
func parsedSliceHandler(element *typeHandler) *typeHandler {
	t := reflect.SliceOf(element.baseType)
	parse := func(name string, items []string) (any, error) {
		values := reflect.MakeSlice(t, 0, len(items))
		for _, text := range items {
			value, err := element.parse(name, text)
			if err != nil {
				return nil, err
			}
			values = reflect.Append(values, reflect.ValueOf(value).Elem())
		}
		ptr := reflect.New(t)
		ptr.Elem().Set(values)
		return ptr.Interface(), nil
	}
	return &typeHandler{
		baseType: t,
		bindFlag: func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			var items []string
			if defaultVal != nil {
				values := reflect.ValueOf(defaultVal).Elem()
				for i := 0; i < values.Len(); i++ {
					items = append(items, element.format(values.Index(i)))
				}
			}
			return cmd.Flags().StringSliceP(name, short, items, descr)
		},
		parse: func(name, text string) (any, error) {
			items, err := parseSliceWith(text, parseString)
			if err != nil {
				return nil, err
			}
			return parse(name, *items)
		},
		convert: func(name string, value any) (any, error) {
			if items, ok := value.(*[]string); ok {
				return parse(name, *items)
			}
			return value, nil
		},
	}
}

// jsonFallbackHandler creates a handler for any type that uses StringP for cobra binding
// and json.Unmarshal for parsing. This handles nested slices, complex maps, and any
// other type that Go's JSON decoder can handle.
func jsonFallbackHandler(t reflect.Type) *typeHandler {
	return parsedStringHandler(t, func(text string) (reflect.Value, error) {
		value := reflect.New(t)
		err := UnmarshalJSON([]byte(text), value.Interface())
		return value.Elem(), err
	}, func(value reflect.Value) string { data, _ := json.Marshal(value.Interface()); return string(data) })
}

// lookupMapHandler dynamically builds a handler for map[string]V types by composing
// the value type's scalar handler for parsing. For cobra flag binding, it uses pflag's
// native StringToString/StringToInt/StringToInt64 methods where available, falling back
// to a StringP flag with key=value parsing.
func lookupMapHandler(t reflect.Type) *typeHandler {
	if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
		return nil // only map[string]V is supported
	}

	valType := normalizeType(t.Elem())

	// Find the scalar handler for the value type
	valHandler := lookupHandler(valType)
	if valHandler == nil {
		return nil
	}

	parseFn := buildMapParse(t, valType, valHandler)

	// Build a composed handler
	h := &typeHandler{
		baseType: t,
		bindFlag: buildMapBindFlag(t, valType),
		parse:    parseFn,
	}

	// Non-native map types (not string/int/int64) use StringP, so they need a
	// convert function to parse the stored string into the actual map type.
	switch valType {
	case reflect.TypeOf(""), reflect.TypeOf(0), reflect.TypeOf(int64(0)):
		// Native pflag support — no convert needed
	default:
		h.convert = func(name string, val any) (any, error) {
			if strPtr, ok := val.(*string); ok {
				if *strPtr == "" {
					return val, nil
				}
				return parseFn(name, *strPtr)
			}
			return val, nil // already converted
		}
	}

	return h
}

// buildMapBindFlag returns a bindFlag function for map[string]V.
// Uses pflag's native methods for string/int/int64, falls back to StringP for others.
func buildMapBindFlag(mapType, valType reflect.Type) func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
	// Check for pflag's native map support
	switch valType {
	case reflect.TypeOf(""):
		return func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			var def map[string]string
			if defaultVal != nil {
				def = reflect.ValueOf(defaultVal).Elem().Interface().(map[string]string)
			}
			return cmd.Flags().StringToStringP(name, short, def, descr)
		}
	case reflect.TypeOf(0):
		return func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			var def map[string]int
			if defaultVal != nil {
				def = reflect.ValueOf(defaultVal).Elem().Interface().(map[string]int)
			}
			return cmd.Flags().StringToIntP(name, short, def, descr)
		}
	case reflect.TypeOf(int64(0)):
		return func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			var def map[string]int64
			if defaultVal != nil {
				def = reflect.ValueOf(defaultVal).Elem().Interface().(map[string]int64)
			}
			return cmd.Flags().StringToInt64P(name, short, def, descr)
		}
	default:
		// Fall back to string flag with custom parsing
		return func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			def := ""
			return cmd.Flags().StringP(name, short, def, descr)
		}
	}
}

// buildMapParse returns a parse function for map[string]V that delegates
// value parsing to the scalar handler.
func buildMapParse(mapType, valType reflect.Type, valHandler *typeHandler) func(name, strVal string) (any, error) {
	return func(name, strVal string) (any, error) {
		result := reflect.MakeMap(mapType)
		if strVal == "" {
			ptr := reflect.New(mapType)
			ptr.Elem().Set(result)
			return ptr.Interface(), nil
		}
		pairs := strings.Split(strVal, ",")
		for _, pair := range pairs {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid map entry for param %s: %q (expected key=value)", name, pair)
			}
			key := strings.TrimSpace(kv[0])
			valStr := strings.TrimSpace(kv[1])
			parsedPtr, err := valHandler.parse(name, valStr)
			if err != nil {
				return nil, fmt.Errorf("invalid value for param %s key %q: %w", name, key, err)
			}
			// parsedPtr is *V, dereference to get V
			result.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(parsedPtr).Elem())
		}
		ptr := reflect.New(mapType)
		ptr.Elem().Set(result)
		return ptr.Interface(), nil
	}
}

// typedIntSliceValue is a pflag.Value / pflag.SliceValue implementation for integer
// slice types pflag has no native slice flag for (uint8, uint16, uint32, uint64,
// int8, int16). Same outward behavior as Int32SliceP / Int64SliceP: comma-split,
// repeated-flag, CSV-with-quotes — pflag handles all of that via Set().
type typedIntSliceValue[T any] struct {
	value      *[]T
	changed    bool
	parseElem  func(string) (T, error)
	formatElem func(T) string
	typeName   string
}

func (s *typedIntSliceValue[T]) Set(val string) error {
	parts, err := readAsCSV(val)
	if err != nil {
		return err
	}
	parsed := make([]T, 0, len(parts))
	for _, p := range parts {
		v, err := s.parseElem(p)
		if err != nil {
			return err
		}
		parsed = append(parsed, v)
	}
	if !s.changed {
		*s.value = parsed
	} else {
		*s.value = append(*s.value, parsed...)
	}
	s.changed = true
	return nil
}

func (s *typedIntSliceValue[T]) Type() string { return s.typeName }

func (s *typedIntSliceValue[T]) String() string {
	parts := make([]string, len(*s.value))
	for i, v := range *s.value {
		parts[i] = s.formatElem(v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (s *typedIntSliceValue[T]) Append(val string) error {
	v, err := s.parseElem(val)
	if err != nil {
		return err
	}
	*s.value = append(*s.value, v)
	return nil
}

func (s *typedIntSliceValue[T]) Replace(vals []string) error {
	parsed := make([]T, 0, len(vals))
	for _, str := range vals {
		v, err := s.parseElem(str)
		if err != nil {
			return err
		}
		parsed = append(parsed, v)
	}
	*s.value = parsed
	return nil
}

func (s *typedIntSliceValue[T]) GetSlice() []string {
	parts := make([]string, len(*s.value))
	for i, v := range *s.value {
		parts[i] = s.formatElem(v)
	}
	return parts
}

// makeIntSliceFallbackHandler builds a slice handler for integer types pflag has no
// native slice flag for. Uses Var() with a typedIntSliceValue so the resulting flag
// is a proper typed slice (same shape as Int32SliceP) — no string round-trip, no
// convert function needed.
func makeIntSliceFallbackHandler[T any](
	elemType reflect.Type,
	typeName string,
	parseElem func(string) (T, error),
	formatElem func(T) string,
) *typeHandler {
	return &typeHandler{
		baseType: reflect.SliceOf(elemType),
		bindFlag: func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			storage := new([]T)
			if defaultVal != nil {
				if typed := toTypedSlice[T](derefSliceDefault(defaultVal)); typed != nil {
					*storage = typed
				}
			}
			v := &typedIntSliceValue[T]{
				value:      storage,
				parseElem:  parseElem,
				formatElem: formatElem,
				typeName:   typeName,
			}
			cmd.Flags().VarP(v, name, short, descr)
			return storage
		},
		parse: func(name, strVal string) (any, error) {
			return parseSliceWith(strVal, parseElem)
		},
	}
}

// reflectArrayValue implements pflag.Value for array collection mode. Unlike
// the normal slice values, Set parses its entire argument as one scalar. The
// first CLI occurrence replaces defaults and later occurrences append, which
// matches pflag's native StringArray behavior.
type reflectArrayValue struct {
	value       reflect.Value // pointer to a canonical []T
	elemHandler *typeHandler
	name        string
	typeName    string
	changed     bool
}

func (v *reflectArrayValue) Set(raw string) error {
	parsed, err := v.elemHandler.parse(v.name, raw)
	if err != nil {
		return err
	}
	elem := reflect.ValueOf(parsed).Elem()
	slice := v.value.Elem()
	if !v.changed {
		slice = reflect.MakeSlice(slice.Type(), 0, 1)
	}
	v.value.Elem().Set(reflect.Append(slice, elem))
	v.changed = true
	return nil
}

func (v *reflectArrayValue) Type() string { return v.typeName + "Array" }

func (v *reflectArrayValue) String() string {
	parts := v.GetSlice()
	return "[" + strings.Join(parts, ",") + "]"
}

func (v *reflectArrayValue) Append(raw string) error {
	parsed, err := v.elemHandler.parse(v.name, raw)
	if err != nil {
		return err
	}
	v.value.Elem().Set(reflect.Append(v.value.Elem(), reflect.ValueOf(parsed).Elem()))
	return nil
}

func (v *reflectArrayValue) Replace(values []string) error {
	v.value.Elem().Set(reflect.MakeSlice(v.value.Elem().Type(), 0, len(values)))
	for _, raw := range values {
		parsed, err := v.elemHandler.parse(v.name, raw)
		if err != nil {
			return err
		}
		v.value.Elem().Set(reflect.Append(v.value.Elem(), reflect.ValueOf(parsed).Elem()))
	}
	return nil
}

func (v *reflectArrayValue) GetSlice() []string {
	slice := v.value.Elem()
	parts := make([]string, slice.Len())
	for i := range parts {
		if format := v.elemHandler.format; format != nil {
			parts[i] = format(slice.Index(i))
		} else {
			parts[i] = fmt.Sprint(slice.Index(i).Interface())
		}
	}
	return parts
}

// bindArrayFlag binds one-value-per-occurrence semantics for every scalar
// slice type Boa supports. []string deliberately uses pflag's native helper;
// other scalar slices use reflectArrayValue because pflag has no matching
// Array helpers.
func bindArrayFlag(cmd *cobra.Command, name, short, descr string, elemType reflect.Type, defaultVal any) (any, error) {
	if elemType == reflect.TypeOf("") {
		return cmd.Flags().StringArrayP(name, short, toTypedSlice[string](derefSliceDefault(defaultVal)), descr), nil
	}

	elemHandler := lookupHandler(elemType)
	if elemHandler == nil {
		return nil, fmt.Errorf("collection mode %q is not supported for slice param %s with element type %s", CollectionArray, name, elemType)
	}
	storageType := reflect.SliceOf(elemHandler.baseType)
	storage := reflect.New(storageType)
	if defaultVal != nil {
		def := reflect.ValueOf(defaultVal).Elem()
		if def.Type().ConvertibleTo(storageType) {
			storage.Elem().Set(def.Convert(storageType))
		}
	}
	value := &reflectArrayValue{
		value:       storage,
		elemHandler: elemHandler,
		name:        name,
		typeName:    elemHandler.baseType.String(),
	}
	cmd.Flags().VarP(value, name, short, descr)
	return storage.Interface(), nil
}

// readAsCSV parses one value pflag receives from the command line into individual
// elements, using csv rules so quoted commas survive (matches pflag's own
// stringSliceValue behavior).
func readAsCSV(val string) ([]string, error) {
	if val == "" {
		return nil, nil
	}
	stringReader := csv.NewReader(strings.NewReader(val))
	return stringReader.Read()
}

// derefSliceDefault dereferences a defaultVal pointer (e.g., *[]string → []string) for slice handlers.
// defaultVal comes from paramMeta.defaultValuePtr() which always returns a pointer.
func derefSliceDefault(defaultVal any) any {
	if defaultVal == nil {
		return nil
	}
	v := reflect.ValueOf(defaultVal)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		return v.Elem().Interface()
	}
	return defaultVal
}

// parseSliceWith is a generic helper that parses a bracketed string "[a,b,c]" into a typed slice.
func parseSliceWith[T any](strVal string, parseFn func(string) (T, error)) (*[]T, error) {
	strVal = strings.TrimSpace(strVal)
	if strings.HasPrefix(strVal, "[") && strings.HasSuffix(strVal, "]") {
		strVal = strVal[1 : len(strVal)-1]
	}
	if strVal == "" {
		result := make([]T, 0)
		return &result, nil
	}
	parts := strings.Split(strVal, ",")
	result := make([]T, len(parts))
	for i, part := range parts {
		v, err := parseFn(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return &result, nil
}

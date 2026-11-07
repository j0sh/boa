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
	handler := nativeHandler(bind, nil)
	handler.parse = func(name, text string) (any, error) { return parseSliceWith(text, parse) }
	return handler
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
	switch valType {
	case reflect.TypeFor[string]():
		return nativeHandler((*pflag.FlagSet).StringToStringP, nil).bindFlag
	case reflect.TypeFor[int]():
		return nativeHandler((*pflag.FlagSet).StringToIntP, nil).bindFlag
	case reflect.TypeFor[int64]():
		return nativeHandler((*pflag.FlagSet).StringToInt64P, nil).bindFlag
	default:
		return func(cmd *cobra.Command, name, short, descr string, defaultVal any) any {
			return cmd.Flags().StringP(name, short, "", descr)
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

// makeIntSliceFallbackHandler retains typed storage for integer slices without
// a native pflag helper. Array and CSV modes share the same SliceValue backend.
func makeIntSliceFallbackHandler[T any](elemType reflect.Type, typeName string, parseElem func(string) (T, error), formatElem func(T) string) *typeHandler {
	element := &typeHandler{
		parse: func(_ string, text string) (any, error) {
			value, err := parseElem(text)
			return new(value), err
		},
		format: func(value reflect.Value) string { return formatElem(value.Interface().(T)) },
	}
	return nativeSliceHandler(func(flags *pflag.FlagSet, name, short string, defaults []T, descr string) *[]T {
		storage := new(defaults)
		flags.VarP(&reflectSliceValue{value: reflect.ValueOf(storage), elemHandler: element, name: name, typeName: typeName, csv: true}, name, short, descr)
		return storage
	}, parseElem)
}

// reflectSliceValue preserves pflag's first-set/repeated-set behavior, with CSV
// splitting for integer slices and one scalar per occurrence for array mode.
type reflectSliceValue struct {
	value          reflect.Value
	elemHandler    *typeHandler
	name, typeName string
	changed, csv   bool
}

func (v *reflectSliceValue) appendValues(dst reflect.Value, values []string) error {
	for _, raw := range values {
		parsed, err := v.elemHandler.parse(v.name, raw)
		if err != nil {
			return err
		}
		dst.Set(reflect.Append(dst, reflect.ValueOf(parsed).Elem()))
	}
	return nil
}

func (v *reflectSliceValue) Set(raw string) error {
	values := []string{raw}
	if v.csv {
		var err error
		values, err = readAsCSV(raw)
		if err != nil {
			return err
		}
	}
	parsed := reflect.MakeSlice(v.value.Elem().Type(), 0, len(values))
	// Slice headers must be settable while elements are appended.
	dst := reflect.New(parsed.Type()).Elem()
	dst.Set(parsed)
	if err := v.appendValues(dst, values); err != nil {
		return err
	}
	if v.changed {
		dst.Set(reflect.AppendSlice(v.value.Elem(), dst))
	}
	v.value.Elem().Set(dst)
	v.changed = true
	return nil
}

func (v *reflectSliceValue) Type() string   { return v.typeName }
func (v *reflectSliceValue) String() string { return "[" + strings.Join(v.GetSlice(), ",") + "]" }
func (v *reflectSliceValue) Append(raw string) error {
	return v.appendValues(v.value.Elem(), []string{raw})
}

func (v *reflectSliceValue) Replace(values []string) error {
	dst := v.value.Elem()
	// CSV replacement is transactional; array mode retains its partial-write semantics.
	if v.csv {
		dst = reflect.New(dst.Type()).Elem()
	}
	dst.Set(reflect.MakeSlice(dst.Type(), 0, len(values)))
	if err := v.appendValues(dst, values); err != nil {
		return err
	}
	v.value.Elem().Set(dst)
	return nil
}

func (v *reflectSliceValue) GetSlice() []string {
	values := v.value.Elem()
	parts := make([]string, values.Len())
	for i := range parts {
		if format := v.elemHandler.format; format != nil {
			parts[i] = format(values.Index(i))
		} else {
			parts[i] = fmt.Sprint(values.Index(i).Interface())
		}
	}
	return parts
}

// bindArrayFlag binds one-value-per-occurrence semantics for every scalar
// slice type Boa supports. []string deliberately uses pflag's native helper;
// other scalar slices use reflectSliceValue because pflag has no matching
// Array helpers.
func bindArrayFlag(cmd *cobra.Command, name, short, descr string, elemType reflect.Type, defaultVal any) (any, error) {
	if elemType == reflect.TypeOf("") {
		return nativeHandler((*pflag.FlagSet).StringArrayP, nil).bindFlag(cmd, name, short, descr, defaultVal), nil
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
	value := &reflectSliceValue{
		value:       storage,
		elemHandler: elemHandler,
		name:        name,
		typeName:    elemHandler.baseType.String() + "Array",
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

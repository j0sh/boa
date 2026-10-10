package boa

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// noConfigPredicate resolves policy at a declared field path relative to the
// load target. Command metadata overrides tags; standalone loads use tags alone.
type noConfigPredicate func(fieldPath, reflect.StructField) bool

func (ctx *processingContext) noConfig(path fieldPath, sf reflect.StructField) bool {
	if ctx != nil {
		if mirror := ctx.mirrorByPath[path]; mirror != nil {
			return mirror.IsNoConfig()
		}
	}
	return slices.Contains(getBoaTags(sf), "noconfig") || sf.Tag.Get("secret") == "true"
}

type configField struct {
	path     fieldPath
	keys     []string
	name     string
	noConfig bool
	ignored  bool
}

// configFields is the common mapping for input inspection and managed dumps.
// It includes named groups (even empty ones), respects format embedding rules, and
// retains ignored fields for rejection while excluding them from source tracking.
func configFields(t reflect.Type, tag string, policy noConfigPredicate) []configField {
	if policy == nil {
		policy = (*processingContext)(nil).noConfig
	}
	var fields []configField
	visiting := map[reflect.Type]bool{}
	var walk func(reflect.Type, []int, configField)
	walk = func(t reflect.Type, index []int, parent configField) {
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == nil || t.Kind() != reflect.Struct || visiting[t] {
			return
		}
		visiting[t] = true
		defer delete(visiting, t)
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			tagParts := strings.Split(sf.Tag.Get(tag), ",")
			key := tagParts[0]
			ft := sf.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			// JSON promotes exported fields of unexported anonymous structs too.
			if key == "-" || !sf.IsExported() && (tag != "json" || !sf.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			childIndex := append(slices.Clone(index), i)
			f := configField{
				path: joinPath(childIndex), keys: parent.keys,
				name:    strings.TrimPrefix(parent.name+"."+sf.Name, "."),
				ignored: parent.ignored || isBoaIgnored(sf),
			}
			f.noConfig = parent.noConfig || policy(f.path, sf)
			group := ft.Kind() == reflect.Struct && !isSupportedType(sf.Type)
			flatten := sf.Anonymous && key == ""
			if tag == "yaml" {
				flatten = slices.Contains(tagParts[1:], "inline")
			}
			if !group || !flatten {
				if key == "" {
					key = sf.Name
					if tag == "yaml" {
						key = strings.ToLower(key)
					}
				}
				f.keys = append(slices.Clone(parent.keys), key)
				fields = append(fields, f)
			}
			if group {
				walk(sf.Type, childIndex, f)
			}
		}
	}
	walk(t, nil, configField{})
	return fields
}

type configInput struct {
	path    fieldPath
	nonNull bool
}

// inspectConfig rejects forbidden keys before decoding and records supplied fields.
// Nulls count as input but do not replace the origin of a retained scalar value.
func inspectConfig(data []byte, target any, format ConfigFormat, tag string, policy noConfigPredicate, rejectUnknown bool) ([]configInput, error) {
	fields := configFields(reflect.TypeOf(target), tag, policy)
	if !rejectUnknown && policy == nil && !slices.ContainsFunc(fields, func(f configField) bool { return f.noConfig }) {
		return nil, nil // Standalone decoders need not support a key probe otherwise.
	}
	var raw map[string]any
	probeErr := fmt.Errorf("config format does not provide KeyTree")
	if format.KeyTree != nil {
		raw, probeErr = format.KeyTree(data)
		if probeErr == nil && raw == nil {
			probeErr = fmt.Errorf("KeyTree returned nil")
		}
	}
	if probeErr != nil {
		purpose := "automatic loading"
		if rejectUnknown {
			purpose = "RejectUnknown"
		} else if policy == nil {
			purpose = `boa:"noconfig"`
		}
		return nil, fmt.Errorf("%s requires a usable ConfigFormat.KeyTree: %w", purpose, probeErr)
	}
	var present []configInput
	for _, f := range fields {
		if actual, nonNull := configKeyPath(raw, f.keys); actual != nil {
			if f.noConfig {
				return nil, fmt.Errorf("config key %q is forbidden by boa:\"noconfig\" on field %s", strings.Join(actual, "."), f.name)
			}
			if !f.ignored {
				present = append(present, configInput{f.path, nonNull})
			}
		}
	}
	if rejectUnknown {
		if err := checkConfigKeys(raw, reflect.TypeOf(target), tag, ""); err != nil {
			return nil, err
		}
	}
	return present, nil
}

// Inspect case-equivalent branches and retain any non-null assignment. A null
// spelling must not hide another spelling that supplies a path in the same file.
func configKeyPath(raw map[string]any, path []string) ([]string, bool) {
	var matches []string
	for key := range raw {
		if strings.EqualFold(key, path[0]) {
			matches = append(matches, key)
		}
	}
	slices.Sort(matches)
	var present []string
	for _, key := range matches {
		rest, nonNull := []string{}, raw[key] != nil
		if len(path) > 1 {
			rest, nonNull = configKeyPath(asKeyMap(raw[key]), path[1:])
			if rest == nil {
				continue
			}
		}
		present = append([]string{key}, rest...)
		if nonNull {
			return present, true
		}
	}
	return present, false
}

// asKeyMap also accepts the nested mapping shape produced by YAML v2.
func asKeyMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			if ks, ok := k.(string); ok {
				out[ks] = val
			}
		}
		return out
	}
	return nil
}

func markConfigKeysPresent(ctx *processingContext, targetPath fieldPath, present []configInput, file string) {
	for _, input := range present {
		path := input.path
		if targetPath != "" {
			path = targetPath + "." + path
		}
		v, ok := ctx.resolveFieldValue(path)
		if !ok {
			continue // A later overlay may clear an enclosing pointer group.
		}
		if pm, ok := ctx.mirrorByPath[path].(*paramMeta); ok {
			if !pm.ignored {
				pm.setByConfig = true
				if input.nonNull {
					pm.configSource = file
				}
				if !pm.wasSetOnCli() && !pm.wasSetByEnv() {
					value := v
					if pm.isPointer {
						value = reflect.Indirect(value)
					}
					if value.IsValid() {
						// Preserve explicit zero values when mirrors synchronize.
						pm.setValuePtr(reinterpretAs(value, pm.GetType()).Addr().Interface())
					}
				}
			}
		} else if v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.Struct {
			markStructPtrPresentByConfig(ctx, path)
		}
	}
}

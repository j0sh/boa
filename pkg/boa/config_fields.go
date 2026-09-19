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
	return slices.Contains(getBoaTags(sf), "noconfig")
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
			if !sf.IsExported() || key == "-" {
				continue
			}
			childIndex := append(slices.Clone(index), i)
			f := configField{
				path: joinPath(childIndex), keys: parent.keys,
				name:    strings.TrimPrefix(parent.name+"."+sf.Name, "."),
				ignored: parent.ignored || isBoaIgnored(sf),
			}
			f.noConfig = parent.noConfig || policy(f.path, sf)
			ft := sf.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
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

// inspectConfig probes once, rejects forbidden keys before decoding, and returns
// declared paths to mark after all overlays. Nil means snapshot fallback; an
// empty non-nil slice means a successful probe with no recognized keys.
func inspectConfig(data []byte, target any, format ConfigFormat, tag string, policy noConfigPredicate) ([]fieldPath, error) {
	fields := configFields(reflect.TypeOf(target), tag, policy)
	if policy == nil && !slices.ContainsFunc(fields, func(f configField) bool { return f.noConfig }) {
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
	var present []fieldPath
	if probeErr == nil {
		present = []fieldPath{}
	}
	for _, f := range fields {
		if f.noConfig && probeErr != nil {
			return nil, fmt.Errorf("cannot inspect config for boa:\"noconfig\" on field %s: %w", f.name, probeErr)
		}
		if actual, ok := configKeyPathPresent(raw, f.keys); probeErr == nil && ok {
			if f.noConfig {
				return nil, fmt.Errorf("config key %q is forbidden by boa:\"noconfig\" on field %s", strings.Join(actual, "."), f.name)
			}
			if !f.ignored {
				present = append(present, f.path)
			}
		}
	}
	return present, nil
}

// Inspect every case-equivalent branch: JSON can decode multiple spellings into
// the same struct. Sort matches to keep error reporting deterministic.
func configKeyPathPresent(raw map[string]any, path []string) ([]string, bool) {
	var matches []string
	for key := range raw {
		if strings.EqualFold(key, path[0]) {
			matches = append(matches, key)
		}
	}
	slices.Sort(matches)
	for _, key := range matches {
		if len(path) == 1 {
			return []string{key}, true
		}
		if rest, ok := configKeyPathPresent(asKeyMap(raw[key]), path[1:]); ok {
			return append([]string{key}, rest...), true
		}
	}
	return nil, false
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

func markConfigKeysPresent(ctx *processingContext, targetPath fieldPath, present []fieldPath) {
	for _, path := range present {
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
			}
		} else if v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.Struct {
			markStructPtrPresentByConfig(ctx, path)
		}
	}
}

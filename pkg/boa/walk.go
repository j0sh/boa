package boa

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// fieldWalker discovers fields for commands, defaults, and validation. Adapters
// decide which fields are leaves and supply metadata, hooks, and error policy.
type fieldWalker struct {
	preallocated *[]preallocatedPtrInfo // non-nil only during initial command discovery
	indices      []int
	prefix       string
	onStruct     func(reflect.Value) error
	active       map[configCopyKey]bool
	remaining    int
	exhausted    bool
	failures     *ValidationError
}

func (w *fieldWalker) done() bool {
	return w.exhausted || w.failures != nil && w.failures.Truncated
}

func (w *fieldWalker) fail(path string, code ValidationCode, message string) error {
	failure := &FieldError{Path: path, Code: code, Err: errors.New(message)}
	if w.failures != nil {
		w.failures.append("", failure)
		return nil
	}
	return failure
}

func (w *fieldWalker) walk(v reflect.Value, path string, depth int, visit func(reflect.StructField, reflect.Value, string) (bool, error)) error {
	if w.done() || v.Kind() == reflect.Interface && (v.IsNil() || !mayHaveFields(v.Elem().Type())) {
		return nil
	}
	// Parameter discovery is outside validation; defaults and the shared
	// validator supply a bounded work budget.
	if w.remaining >= 0 {
		if w.remaining == 0 {
			w.exhausted = true
			return w.fail(path, CodeLimit, fmt.Sprintf("traversal exceeds %d steps", maxFieldSteps))
		}
		w.remaining--
		if depth > maxFieldDepth {
			return w.fail(path, CodeLimit, fmt.Sprintf("nesting exceeds %d recursive steps", maxFieldDepth))
		}
	}
	if depth != 0 && lookupHandler(v.Type()) != nil {
		return nil
	}
	switch v.Kind() {
	case reflect.Struct:
		if w.preallocated == nil {
			break
		}
		key := configCopyKey{typ: v.Type()}
		if w.active[key] {
			return fmt.Errorf("recursive parameter group %s at %s: mark the recursive field boa:\"ignore\" or register a scalar parser", v.Type(), joinPath(w.indices))
		}
		w.active[key] = true
		defer delete(w.active, key)
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			if w.preallocated == nil || v.Kind() != reflect.Pointer || v.Type().Elem().Kind() != reflect.Struct {
				return nil
			}
			v.Set(reflect.New(v.Type().Elem()))
			defer func() {
				*w.preallocated = append(*w.preallocated, preallocatedPtrInfo{path: joinPath(w.indices)})
			}()
		}
		key := configCopyKey{typ: v.Type(), ptr: v.UnsafePointer()}
		if v.Kind() == reflect.Slice {
			key.len = v.Len()
		}
		if w.active[key] && w.preallocated == nil {
			return nil
		}
		w.active[key] = true
		defer delete(w.active, key)
	}
	walk := func(child reflect.Value, childPath string) error { return w.walk(child, childPath, depth+1, visit) }
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
		if w.onStruct != nil {
			if err := w.onStruct(v); err != nil {
				return err
			}
		}
		for i := 0; i < v.NumField() && !w.done(); i++ {
			field := v.Type().Field(i)
			if isBoaIgnored(field) {
				continue
			}
			childPath := field.Name
			if path != "" {
				childPath = path + "." + childPath
			}
			child := v.Field(i)
			w.indices = append(w.indices, i)
			descend, err := visit(field, child, childPath)
			if err == nil && descend {
				prefix := w.prefix
				if !field.Anonymous {
					w.prefix += field.Name
				}
				err = walk(child, childPath)
				w.prefix = prefix
			}
			w.indices = w.indices[:len(w.indices)-1]
			if err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if !mayHaveFields(v.Type().Elem()) {
			return nil
		}
		for i := 0; i < v.Len() && !w.done(); i++ {
			if err := walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return w.fail(path, CodeSchema, "expected string map keys")
		}
		if !mayHaveFields(v.Type().Elem()) {
			return nil
		}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
		for _, key := range keys {
			if w.done() {
				break
			}
			child := reflect.New(v.Type().Elem()).Elem()
			child.Set(v.MapIndex(key))
			if err := walk(child, path+"["+strconv.Quote(key.String())+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

// Skip scalar collections without allocating element paths. Recursive type
// shapes remain subject to the ordinary value-traversal limits.
func mayHaveFields(t reflect.Type) bool {
	for range maxFieldDepth {
		if lookupHandler(t) != nil {
			return false
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			t = t.Elem()
		case reflect.Map:
			if t.Key().Kind() != reflect.String {
				return true
			}
			t = t.Elem()
		case reflect.Struct, reflect.Interface:
			return true
		default:
			return false
		}
	}
	return true
}

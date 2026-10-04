package boa

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
)

type baseDirOptions struct {
	enabled, required, autoCreate bool
}

func parseBaseDirTag(value string) (baseDirOptions, error) {
	var options baseDirOptions
	seen := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if seen[item] {
			return options, fmt.Errorf("repeated option %q", item)
		}
		seen[item] = true
		switch item {
		case "true":
			options.enabled = true
		case "false":
		case "required":
			options.enabled, options.required = true, true
		case "autocreate":
			options.enabled, options.autoCreate = true, true
		default:
			return options, fmt.Errorf("invalid option %q (expected true, false, required, or autocreate)", item)
		}
	}
	if seen["false"] && len(seen) != 1 {
		return options, fmt.Errorf("false cannot be combined with other options")
	}
	return options, nil
}

type baseDirState struct {
	dir, name string
	options   baseDirOptions
}

// pathInvocation belongs to one executed Cobra command. Ancestor pipelines
// share its original directory and export only persistent base declarations.
type pathInvocation struct {
	directory string
	dirErr    error
	inherited baseDirState
	visited   map[*cobra.Command]bool
}

type pathInvocationKey struct{}

func (ctx *processingContext) beginPathInvocation(executed, declaring *cobra.Command) {
	invocation, _ := executed.Context().Value(pathInvocationKey{}).(*pathInvocation)
	if invocation == nil || invocation.visited[declaring] {
		invocation = &pathInvocation{visited: map[*cobra.Command]bool{}}
		invocation.directory, invocation.dirErr = os.Getwd()
		executed.SetContext(context.WithValue(executed.Context(), pathInvocationKey{}, invocation))
	}
	invocation.visited[declaring] = true
	ctx.pathInvocation = invocation
	ctx.inheritedBaseDir = invocation.inherited
}

func resolvePath(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func (ctx *processingContext) selectBaseDir(createDirs bool) error {
	ctx.baseDir, ctx.selectedBaseDir = ctx.inheritedBaseDir, nil
	provider := ctx.baseDirProvider
	if provider != nil && !provider.IsIgnored() {
		if provider.IsEnabled() || ctx.baseDir.dir == "" {
			if ctx.pathInvocation.dirErr != nil {
				return NewUserInputError(fmt.Errorf("basedir %s: %w", provider.GetName(), ctx.pathInvocation.dirErr))
			}
			ctx.baseDir = baseDirState{dir: ctx.pathInvocation.directory, name: provider.GetName()}
		}
		if provider.IsEnabled() {
			missing := provider.IsRequired() && !provider.HasValue()
			for _, group := range ctx.PreallocatedPtrs {
				if provider.pathKey.hasSubtreePrefix(group.path) && !ctx.groupHasInput(group.path) {
					missing = false // Normal validation applies if config later activates the group.
				}
			}
			if missing {
				return NewUserInputErrorf("missing required param '%s'", provider.GetName())
			}
			ctx.selectedBaseDir, ctx.baseDir.options = provider, provider.baseDir
			if provider.HasValue() {
				path := reflect.ValueOf(provider.valuePtrF()).Elem().String()
				ctx.baseDir.dir = resolvePath(ctx.pathInvocation.directory, cmp.Or(path, "."))
				ctx.storeValue(provider, reflect.ValueOf(ctx.baseDir.dir))
			}
		}
	}
	var err error
	if ctx.baseDir.options.autoCreate && createDirs {
		err = os.MkdirAll(ctx.baseDir.dir, 0o700)
	}
	if err == nil && ctx.baseDir.options.required {
		var info os.FileInfo
		info, err = os.Stat(ctx.baseDir.dir)
		if err == nil && !info.IsDir() {
			err = fmt.Errorf("not a directory")
		}
	}
	if err != nil {
		return NewUserInputError(fmt.Errorf("basedir %s (%s): %w", ctx.baseDir.name, ctx.baseDir.dir, err))
	}
	if provider != nil && provider.IsPersistent() && !provider.IsIgnored() {
		ctx.pathInvocation.inherited = ctx.baseDir
	}
	return nil
}

func (ctx *processingContext) normalizePath(param parameter, fromField bool) {
	if ctx.baseDir.dir == "" || param.IsIgnored() || !param.IsEnabled() {
		return
	}
	field, _ := ctx.resolveFieldValue(param.(*paramMeta).pathKey)
	field = reflect.Indirect(field)
	if !field.IsValid() || !param.HasValue() && (!fromField || field.IsZero()) {
		return
	}
	value := field
	if !fromField {
		value = reflect.ValueOf(param.valuePtrF()).Elem()
	}
	if value.Kind() == reflect.String {
		ctx.storeValue(param, reflect.ValueOf(resolvePath(ctx.baseDir.dir, value.String())))
	} else if !value.IsNil() {
		paths := reflect.MakeSlice(param.GetType(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			paths.Index(i).SetString(resolvePath(ctx.baseDir.dir, value.Index(i).String()))
		}
		ctx.storeValue(param, paths)
	}
}

func (ctx *processingContext) normalizePaths(fromFields bool) {
	for _, param := range ctx.pathParams {
		ctx.normalizePath(param, fromFields)
	}
}

func (ctx *processingContext) checkFrozenBaseDir() error {
	provider := ctx.selectedBaseDir
	if provider == nil {
		return nil
	}
	field, ok := ctx.resolveFieldValue(provider.pathKey)
	if !ok {
		return nil // An untouched optional group may have been cleaned up.
	}
	value, path := reflect.Indirect(field), ""
	if value.IsValid() {
		path = value.String()
	}
	resolved := resolvePath(ctx.pathInvocation.directory, cmp.Or(path, "."))
	if resolved != ctx.baseDir.dir {
		return NewUserInputErrorf("basedir %s cannot change after source loading (%s to %s)", ctx.baseDir.name, ctx.baseDir.dir, resolved)
	}
	if value.IsValid() && (provider.HasValue() || path != "") {
		ctx.storeValue(provider, reflect.ValueOf(resolved))
	}
	return nil
}

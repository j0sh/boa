package boa

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/spf13/cobra"
)

type baseDirOptions struct {
	enabled, required, autoCreate bool
}

// BasePath selects the directory used to resolve relative string or []string paths.
type BasePath string

const (
	// BasePathDir uses the selected BOA base (the initial base for config paths).
	BasePathDir BasePath = "basedir"
	// BasePathSource uses the original working directory for CLI/env input, the
	// supplying file's directory for config input, and the BOA base for defaults.
	BasePathSource BasePath = "source"
)

func parseBaseDirTag(value string) (baseDirOptions, error) {
	options, err := parsePathTag(value, "true", "false", "required", "autocreate")
	if err != nil {
		return baseDirOptions{}, err
	}
	return baseDirOptions{enabled: !options["false"], required: options["required"], autoCreate: options["autocreate"]}, nil
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

// selectBaseDir leaves the field unchanged until final selection, allowing
// configuration to override a relative default.
func (ctx *processingContext) selectBaseDir(final bool) error {
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
			if _, live := ctx.resolveFieldValue(provider.pathKey); !live {
				return nil // The containing optional group is absent.
			}
			if final && provider.IsRequired() && !provider.HasValue() {
				return NewUserInputErrorf("missing required param '%s'", provider.GetName())
			}
			ctx.selectedBaseDir, ctx.baseDir.options = provider, provider.baseDir
			if provider.HasValue() {
				path := reflect.ValueOf(provider.valuePtrF()).Elem().String()
				base := ctx.pathInvocation.directory
				if final {
					base = ctx.basePathFor(provider)
				}
				ctx.baseDir.dir = resolvePath(base, cmp.Or(path, "."))
				if final {
					ctx.storeValue(provider, reflect.ValueOf(ctx.baseDir.dir))
				}
			}
		}
	}
	return nil
}

func (ctx *processingContext) selectInitialBaseDir() error {
	if ctx.pathInvocation.dirErr != nil {
		for _, param := range ctx.mirrorByPath {
			if param.GetBasePath() == BasePathSource && !param.IsIgnored() {
				return NewUserInputError(fmt.Errorf("basepath %s: %w", param.GetName(), ctx.pathInvocation.dirErr))
			}
		}
	}
	if err := ctx.selectBaseDir(false); err != nil {
		return err
	}
	ctx.initialBaseDir = ctx.baseDir.dir
	ctx.selectedBaseDir = nil
	return nil
}

func (ctx *processingContext) finalizeBaseDir(createDirs bool) error {
	if err := ctx.selectBaseDir(true); err != nil {
		return err
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
	if provider := ctx.baseDirProvider; provider != nil && provider.IsPersistent() && !provider.IsIgnored() {
		ctx.pathInvocation.inherited = ctx.baseDir
	}
	return nil
}

func (ctx *processingContext) normalizePath(param parameter, fromField bool) {
	base := ctx.basePathFor(param)
	if base == "" || param.IsIgnored() || !param.IsEnabled() {
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
		ctx.storeValue(param, reflect.ValueOf(resolvePath(base, value.String())))
	} else if !value.IsNil() {
		paths := reflect.MakeSlice(param.GetType(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			paths.Index(i).SetString(resolvePath(base, value.Index(i).String()))
		}
		ctx.storeValue(param, paths)
	}
}

func (ctx *processingContext) defaultBasePath(param parameter) string {
	if param.IsBaseDir() {
		return ctx.pathInvocation.directory
	}
	if param.IsConfigFile() {
		return ctx.initialBaseDir
	}
	return ctx.baseDir.dir
}

func (ctx *processingContext) basePathFor(param parameter) string {
	base := ctx.defaultBasePath(param)
	meta := param.(*paramMeta)
	if meta.GetBasePath() == BasePathSource {
		switch {
		case meta.wasSetOnCli(), meta.wasSetByEnv():
			return ctx.pathInvocation.directory
		case meta.configSource != "":
			return filepath.Dir(meta.configSource)
		}
	}
	if meta.basePath != "" {
		return cmp.Or(base, ctx.pathInvocation.directory)
	}
	return base
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
	resolved := resolvePath(ctx.basePathFor(provider), cmp.Or(path, "."))
	if resolved != ctx.baseDir.dir {
		return NewUserInputErrorf("basedir %s cannot change after source loading (%s to %s)", ctx.baseDir.name, ctx.baseDir.dir, resolved)
	}
	if value.IsValid() && (provider.HasValue() || path != "") {
		ctx.storeValue(provider, reflect.ValueOf(resolved))
	}
	return nil
}

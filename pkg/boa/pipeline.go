package boa

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/spf13/cobra"
)

// loadAndValidate is shared by execution, Validate, and reload. Source loading
// and validation complete before any action hook is eligible to run.
func (b command) loadAndValidate(ctx *processingContext, cmd *cobra.Command, args []string, createDirs bool) error {
	if b.Params == nil {
		return nil
	}

	// Reset the live-reload path registries at the top of every
	// pipeline run so a reload sees a fresh list that reflects
	// only what this run actually loaded.
	ctx.watchMu.Lock()
	ctx.LoadedConfigFiles = ctx.LoadedConfigFiles[:0]
	ctx.ExtraWatchedConfigFiles = ctx.ExtraWatchedConfigFiles[:0]
	ctx.watchMu.Unlock()

	// Must read env values before running any prevalidate code
	if err := parseEnv(ctx, b.Params); err != nil {
		return err
	}

	syncMirrors(ctx)

	// Finish string-backed flag conversion before base selection or config loading.
	// Native pflag values and env/config values are already typed.
	for _, path := range ctx.pathOrder {
		param := ctx.mirrorByPath[path]
		if !param.HasValue() || !param.IsEnabled() || param.IsIgnored() {
			continue
		}
		if handler := handlerFor(param); handler != nil && handler.convert != nil {
			value, err := handler.convert(param.GetName(), param.valuePtrF())
			if err != nil {
				return NewUserInputError(err)
			}
			param.setValuePtr(value)
		}
	}
	syncMirrors(ctx)
	if b.PreConfigFuncCtx != nil {
		if err := b.PreConfigFuncCtx(newHookContext(ctx), b.Params, cmd, args); err != nil {
			return fmt.Errorf("error in PreConfigFuncCtx: %w", err)
		}
		syncMirrors(ctx)
	}
	if err := ctx.selectInitialBaseDir(); err != nil {
		return err
	}

	if err := b.loadConfigs(ctx); err != nil {
		return err
	}
	if b.PostConfigFuncCtx != nil {
		if err := b.PostConfigFuncCtx(newHookContext(ctx), b.Params, cmd, args); err != nil {
			return fmt.Errorf("error in PostConfigFuncCtx: %w", err)
		}
		syncMirrors(ctx)
	}

	// Clean up preallocated struct pointers that had no fields set.
	// This must happen after all value sources (CLI, env, config) and before
	// validation, so that required-field checks don't fire for unused struct groups.
	if len(ctx.PreallocatedPtrs) > 0 {
		cleanupPreallocatedPtrs(ctx)
	}

	syncMirrors(ctx)
	if err := ctx.finalizeBaseDir(createDirs); err != nil {
		return err
	}
	ctx.normalizePaths(false)
	if err := resolveSecretFiles(ctx); err != nil {
		return NewUserInputError(err)
	}

	// if b.params or any inner struct implements CfgStructPreValidate, call it
	err := traverse(ctx, b.Params, nil, func(innerParams any) error {
		if s, ok := innerParams.(CfgStructPreValidate); ok {
			err := s.PreValidate()
			if err != nil {
				return fmt.Errorf("error in PreValidate: %w", err)
			}
		}
		// context-aware interface
		if s, ok := innerParams.(CfgStructPreValidateCtx); ok {
			hookCtx := newHookContext(ctx)
			err := s.PreValidateCtx(hookCtx)
			if err != nil {
				return fmt.Errorf("error in PreValidateCtx: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Run command validation hooks after struct hooks.
	if b.PreValidateFunc != nil {
		err := b.PreValidateFunc(b.Params, cmd, args)
		if err != nil {
			return fmt.Errorf("error in PreValidateFunc: %w", err)
		}
	}

	// if we have a context-aware pre-validate function, call it
	if b.PreValidateFuncCtx != nil {
		hookCtx := newHookContext(ctx)
		err := b.PreValidateFuncCtx(hookCtx, b.Params, cmd, args)
		if err != nil {
			return fmt.Errorf("error in PreValidateFuncCtx: %w", err)
		}
	}

	if err := ctx.checkFrozenBaseDir(); err != nil {
		return err
	}
	ctx.normalizePaths(true)
	syncMirrors(ctx)
	if err = resolveSecretFiles(ctx); err != nil {
		return NewUserInputError(err)
	}

	if err = validate(ctx, b.Params); err != nil {
		return err
	}

	// Sync mirrors again after validation to copy converted values (e.g., *url.URL from string)
	syncMirrors(ctx)

	// Validation and reload never execute action hooks.
	if b.validateOnly {
		return nil
	}

	// Run action hooks only after successful validation.
	err = traverse(ctx, b.Params, nil, func(innerParams any) error {
		if preExecute, ok := innerParams.(CfgStructPreExecute); ok {
			err := preExecute.PreExecute()
			if err != nil {
				return fmt.Errorf("error in PreExecute: %w", err)
			}
		}
		// context-aware interface
		if preExecuteCtx, ok := innerParams.(CfgStructPreExecuteCtx); ok {
			hookCtx := newHookContext(ctx)
			err := preExecuteCtx.PreExecuteCtx(hookCtx)
			if err != nil {
				return fmt.Errorf("error in PreExecuteCtx: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Run command validation hooks after struct hooks.
	if b.PreExecuteFunc != nil {
		err := b.PreExecuteFunc(b.Params, cmd, args)
		if err != nil {
			return fmt.Errorf("error in PreExecuteFunc: %w", err)
		}
	}

	// if we have a context-aware pre-execute function, call it
	if b.PreExecuteFuncCtx != nil {
		hookCtx := newHookContext(ctx)
		err := b.PreExecuteFuncCtx(hookCtx, b.Params, cmd, args)
		if err != nil {
			return fmt.Errorf("error in PreExecuteFuncCtx: %w", err)
		}
	}

	return nil
}

// configTarget resolves against the current struct, including pointer groups
// replaced by Init or PostCreate hooks.
func (ctx *processingContext) configTarget(path fieldPath) (any, error) {
	if path == "" {
		return ctx.rootStructPtr, nil
	}
	v, ok := ctx.resolveFieldValue(path)
	if !ok {
		return nil, fmt.Errorf("config target %s is unavailable", path)
	}
	if v.Kind() == reflect.Struct {
		return v.Addr().Interface(), nil
	}
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		return v.Interface(), nil
	}
	return nil, fmt.Errorf("config target %s is nil", path)
}

func (b command) loadConfigs(ctx *processingContext) error {
	for _, entry := range ctx.ConfigFiles {
		ctx.normalizePath(entry.mirror, false)
	}
	override := b.ConfigFormat
	// Root files override nested files; each list of paths overlays left to right.
	for _, root := range []bool{false, true} {
		for _, entry := range ctx.ConfigFiles {
			if (entry.targetPath == "") != root {
				continue
			}
			// Earlier files can supply or clear a later config path, including
			// one without a default. CLI/env selections keep their priority.
			if p := entry.mirror; !p.wasSetOnCli() && !p.wasSetByEnv() {
				field, _ := ctx.resolveFieldValue(p.(*paramMeta).pathKey)
				field = reflect.Indirect(field)
				if !field.IsValid() {
					continue
				}
				if !field.IsZero() || p.HasValue() {
					p.injectValuePtr(reinterpretAs(field, p.GetType()).Addr().Interface())
				}
			}
			if !entry.mirror.HasValue() {
				continue
			}
			ctx.normalizePath(entry.mirror, false)

			for _, file := range configFilePaths(entry.mirror.valuePtrF()) {
				if file == "" {
					continue
				}
				if err := validateFile(file); err != nil {
					optional := entry.mirror.IsConfigFileOptional()
					if entry.mirror.IsConfigFileOptionalDefault() {
						base := cmp.Or(ctx.defaultBasePath(entry.mirror), ctx.pathInvocation.directory)
						optional = slices.ContainsFunc(configFilePaths(entry.mirror.defaultValuePtr()), func(path string) bool {
							return path != "" && filepath.Clean(resolvePath(base, path)) == filepath.Clean(resolvePath(base, file))
						})
					}
					if errors.Is(err, os.ErrNotExist) && optional {
						continue
					}
					syncMirrors(ctx)
					return NewUserInputError(fmt.Errorf("configfile %s: %w", entry.mirror.GetName(), err))
				}
				target, err := ctx.configTarget(entry.targetPath)
				if err != nil {
					return err
				}
				predicate := func(path fieldPath, sf reflect.StructField) bool {
					if entry.targetPath != "" {
						path = entry.targetPath + "." + path
					}
					return ctx.noConfig(path, sf)
				}
				// Isolate decoder writes, including private scalar storage, from CLI/env values.
				seen := make(map[configCopyKey]reflect.Value)
				for _, path := range ctx.pathOrder {
					param := ctx.mirrorByPath[path]
					if !param.IsIgnored() && (param.wasSetOnCli() || param.wasSetByEnv()) {
						if field, ok := ctx.resolveFieldValue(path); ok {
							field.Set(cloneConfigValue(field, seen))
						}
					}
				}
				present, err := loadConfigFileInto(file, target, override, predicate, b.RejectUnknown)
				if err != nil {
					syncMirrors(ctx)
					return NewUserInputError(fmt.Errorf("configfile %s: %w", entry.mirror.GetName(), err))
				}
				markConfigKeysPresent(ctx, entry.targetPath, present, resolvePath(ctx.pathInvocation.directory, file))
				syncMirrors(ctx)
				ctx.LoadedConfigFiles = append(ctx.LoadedConfigFiles, file)
			}
		}
	}
	syncMirrors(ctx)
	return nil
}

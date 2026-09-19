package boa

import (
	"fmt"
	"reflect"

	"github.com/spf13/cobra"
)

// loadAndValidate is shared by execution, Validate, and reload. Source loading
// and validation complete before any action hook is eligible to run.
func (b command) loadAndValidate(ctx *processingContext, cmd *cobra.Command, args []string) error {
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

	if err := b.loadConfigs(ctx); err != nil {
		return err
	}

	// Clean up preallocated struct pointers that had no fields set.
	// This must happen after all value sources (CLI, env, config) and before
	// validation, so that required-field checks don't fire for unused struct groups.
	if len(ctx.PreallocatedPtrs) > 0 {
		cleanupPreallocatedPtrs(ctx)
	}

	// Finish string-backed flag conversion before hooks inspect the Go fields.
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

	syncMirrors(ctx)

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
	snapshots := snapshotPreallocatedStructs(ctx)
	override := b.ConfigFormat
	type loadedConfig struct {
		path    fieldPath
		present []fieldPath
	}
	var loaded []loadedConfig
	// Root files override nested files; each list of paths overlays left to right.
	for _, root := range []bool{false, true} {
		for _, entry := range ctx.ConfigFiles {
			if (entry.targetPath == "") != root || !entry.mirror.HasValue() {
				continue
			}
			for _, file := range configFilePathsFromMirror(entry.mirror) {
				if file == "" {
					continue
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
				present, err := loadConfigFileInto(file, target, override, predicate)
				if err != nil {
					return NewUserInputError(fmt.Errorf("configfile %s: %w", entry.mirror.GetName(), err))
				}
				loaded = append(loaded, loadedConfig{entry.targetPath, present})
				ctx.LoadedConfigFiles = append(ctx.LoadedConfigFiles, file)
			}
		}
	}
	syncMirrors(ctx)
	var fallback []fieldPath
	for _, item := range loaded {
		if item.present == nil {
			fallback = append(fallback, item.path)
		}
		markConfigKeysPresent(ctx, item.path, item.present)
	}
	markConfigChangedStructs(ctx, snapshots, fallback)
	return nil
}

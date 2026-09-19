package boa

import (
	"fmt"
	"reflect"
	"strings"
)

func (ctx *processingContext) applyTags(param parameter, tags reflect.StructTag) error {
	secret, hasSecret := tags.Lookup("secret")
	if hasSecret && secret != "true" && secret != "false" {
		return fmt.Errorf("param %s: invalid secret value %q (expected \"true\" or \"false\")", param.GetName(), secret)
	}
	secretFor := tags.Get("secretfor")
	if secret == "true" && secretFor != "" {
		return fmt.Errorf("param %s cannot use both secret:\"true\" and secretfor", param.GetName())
	}
	if secret == "true" {
		if param.GetKind() != reflect.String {
			return fmt.Errorf("secret tag requires a string field, got %s", param.GetType())
		}
		param.SetNoFlag(true)
		param.SetNoConfig(true)
	}
	if secretFor != "" {
		if tags.Get("required") == "true" || tags.Get("optional") == "false" {
			return fmt.Errorf("secretfor param %s is implicitly optional and cannot be required", param.GetName())
		}
		param.SetRequired(false)
		if err := ctx.registerSecretFile(param, secretFor); err != nil {
			return fmt.Errorf("param %s: %w", param.GetName(), err)
		}
	}
	if tags.Get("positional") == "true" {
		param.setPositional(true)
	}
	if value, ok := tags.Lookup("persistent"); ok {
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid persistent value for param %s: %s", param.GetName(), value)
		}
		param.setPersistent(value == "true")
	}
	if param.getDescr() == "" {
		if value, ok := tags.Lookup("descr"); ok {
			param.setDescription(value)
		}
	}
	var envPrefix, flagPrefix string
	if meta, ok := param.(*paramMeta); ok {
		envPrefix, flagPrefix = meta.envPrefix, meta.flagPrefix
	}
	if param.GetEnv() == "" {
		if value, ok := tags.Lookup("env"); ok {
			param.SetEnv(envPrefix + value)
		}
	}
	if param.GetShort() == "" {
		if value, ok := tags.Lookup("short"); ok {
			param.SetShort(value)
		}
	}
	if param.GetName() == "" {
		if value, ok := tags.Lookup("name"); ok {
			param.SetName(flagPrefix + value)
		}
	}
	if value, ok := tags.Lookup("alts"); ok {
		var alternatives []string
		for _, item := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "["), "]"), ",") {
			if item = strings.TrimSpace(item); item != "" {
				alternatives = append(alternatives, item)
			}
		}
		param.SetAlternatives(alternatives)
	}
	if value, ok := tags.Lookup("strict"); ok {
		param.SetStrictAlts(value == "true")
	}
	if value, ok := tags.Lookup("collection"); ok {
		mode := CollectionMode(strings.TrimSpace(value))
		if mode != CollectionSlice && mode != CollectionArray {
			return fmt.Errorf("invalid collection mode %q for param %s (expected %q or %q)", value, param.GetName(), CollectionSlice, CollectionArray)
		}
		if param.GetKind() != reflect.Slice {
			return fmt.Errorf("collection on param %s: only slice fields support collection modes", param.GetName())
		}
		param.SetCollection(mode)
	}
	if !param.hasDefaultValue() {
		if value, ok := tags.Lookup("default"); ok {
			parsed, err := handlerFor(param).parse(param.GetName(), value)
			if err != nil {
				return fmt.Errorf("invalid default value for param %s: %w", param.GetName(), err)
			}
			param.SetDefault(parsed)
		}
	}
	if meta, ok := param.(*paramMeta); ok {
		for _, bound := range []struct {
			tag  string
			dest *any
		}{{"min", &meta.minVal}, {"max", &meta.maxVal}} {
			if value, ok := tags.Lookup(bound.tag); ok {
				parsed, err := parseBoundTag(meta.boundKind(), value)
				if err != nil {
					return fmt.Errorf("invalid %s value for param %s: %w", bound.tag, param.GetName(), err)
				}
				if parsed != nil {
					*bound.dest = parsed
				}
			}
		}
		if pattern, ok := tags.Lookup("pattern"); ok {
			meta.pattern = pattern
		}
	}
	if hasFileTag(tags) && param.GetKind() != reflect.String {
		return fmt.Errorf("file tag requires a string field, got %s", param.GetType())
	}
	for _, directive := range strings.Split(tags.Get("boa"), ",") {
		switch strings.TrimSpace(directive) {
		case "noflag":
			param.SetNoFlag(true)
		case "noenv":
			param.SetNoEnv(true)
		case "configonly":
			param.SetNoFlag(true)
			param.SetNoEnv(true)
		}
	}
	if param.isPositional() && (param.IsNoFlag() || param.IsIgnored()) {
		kind := "ignore"
		if param.IsNoFlag() {
			kind = "noflag"
		}
		return positionalSkipError(param.GetName(), kind)
	}
	if tags.Get("configfile") == "true" {
		param.SetConfigFile(true)
	}
	if param.IsConfigFile() {
		t := param.GetType()
		if t.Kind() != reflect.String && (t.Kind() != reflect.Slice || t.Elem().Kind() != reflect.String) {
			return fmt.Errorf("configfile on param %s: must be a string or []string field", param.GetName())
		}
		var path fieldPath
		if meta, ok := param.(*paramMeta); ok {
			if i := strings.LastIndex(string(meta.pathKey), "."); i >= 0 {
				path = meta.pathKey[:i]
			}
		}
		ctx.ConfigFiles = append(ctx.ConfigFiles, configFileEntry{mirror: param, targetPath: path})
	}
	return nil
}

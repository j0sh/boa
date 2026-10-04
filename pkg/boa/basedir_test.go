package boa

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func baseDirNoop[T any](*T, *cobra.Command, []string) {}
func baseDirOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func baseDirFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	baseDirOK(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

type baseDirText string

func (p *baseDirText) UnmarshalText(text []byte) error {
	*p = baseDirText(strings.TrimPrefix(string(text), "path:"))
	return nil
}

func TestBaseDir_Declaration(t *testing.T) {
	for _, tag := range []string{"true", "false", "required", "autocreate", "required,autocreate", " autocreate, true, required "} {
		options, err := parseBaseDirTag(tag)
		baseDirOK(t, err)
		if options != (baseDirOptions{tag != "false", strings.Contains(tag, "required"), strings.Contains(tag, "autocreate")}) {
			t.Fatalf("%q: %+v", tag, options)
		}
	}
	for _, tag := range []string{"", "yes", "true,", "true,true", "false,required", "required,required", "autocreate,autocreate"} {
		if _, err := parseBaseDirTag(tag); err == nil {
			t.Fatalf("accepted %q", tag)
		}
	}
	for _, tag := range []string{`basedir:"invalid"`, `basedir:"true" file:"true"`, `basedir:"true" configfile:"true"`, `basedir:"true" secret:"true"`, `basedir:"true" secretfor:"Token"`} {
		typ := reflect.StructOf([]reflect.StructField{
			{Name: "Dir", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(tag)},
			{Name: "Token", Type: reflect.TypeFor[string](), Tag: `secret:"true" optional:"true"`},
		})
		if _, err := (command{Params: reflect.New(typ).Interface()}).ToCobraE(); err == nil {
			t.Fatalf("accepted %s", tag)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[[]string](), reflect.TypeFor[struct{}]()} {
		params := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "Dir", Type: typ, Tag: `basedir:"true"`}})).Interface()
		if _, err := (command{Params: params}).ToCobraE(); err == nil {
			t.Fatalf("accepted %s base", typ)
		}
	}
	type Group struct {
		Dir string `basedir:"true"`
	}
	if _, err := (Cmd[struct{ A, B Group }]{}).ToCobraE(); err == nil || !strings.Contains(err.Error(), "multiple basedir") {
		t.Fatalf("duplicate declaration: %v", err)
	}
}

func TestBaseDir_MetadataAndTypedPaths(t *testing.T) {
	type Params struct {
		Dir   *baseDirText `basedir:"required,autocreate"`
		Input *baseDirText `file:"true"`
	}
	dir := t.TempDir()
	input := baseDirFile(t, dir, "input", "data")
	var p Params
	baseDirOK(t, (Cmd[Params]{Params: &p, RunFunc: baseDirNoop[Params], InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
		base := Param(ctx, &p.Dir)
		if !base.IsBaseDirRequired() || !base.IsBaseDirAutoCreate() {
			t.Fatal("tags were not seeded before Init")
		}
		base.SetBaseDir(false)
		if base.IsBaseDirRequired() || base.IsBaseDirAutoCreate() || base.IsNoConfig() {
			t.Fatal("designation was not cleared")
		}
		base.SetBaseDirRequired(true)
		base.SetNoConfig(false)
		if !base.IsBaseDir() || !base.IsNoConfig() {
			t.Fatal("provider must forbid config input")
		}
		return nil
	}}).RunArgsE([]string{"--dir", "path:" + dir, "--input", "path:input"}))
	if p.Dir == nil || string(*p.Dir) != dir || p.Input == nil || string(*p.Input) != input {
		t.Fatalf("typed paths: %+v", p)
	}
}

func TestBaseDir_SourcesAndSecrets(t *testing.T) {
	type Params struct {
		Dir       string `basedir:"true" default:"." env:"BOA_TEST_BASE"`
		Config    string `configfile:"true" default:"config.json"`
		Input     string `file:"true" default:"default" env:"BOA_TEST_INPUT"`
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token" default:"token"`
	}
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"default", "config", "env", "cli"} {
		baseDirFile(t, dir, name, name)
	}
	baseDirFile(t, dir, "token", "secret\n")
	baseDirFile(t, dir, "config.json", `{"Input":"config"}`)
	absolute := baseDirFile(t, t.TempDir(), "absolute", "data")
	for _, tc := range []struct {
		name, env, base string
		args            []string
	}{
		{"default", "", "", []string{"--config="}},
		{"config", "", dir, nil},
		{"env", "env", dir, nil},
		{"cli", "env", "wrong", []string{"--dir", dir, "--input", "cli"}},
		{absolute, "", "missing", []string{"--input", absolute, "--config=", "--token-file", filepath.Join(dir, "token")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_TEST_BASE", tc.base)
			t.Setenv("BOA_TEST_INPUT", tc.env)
			want := resolvePath(dir, tc.name)
			baseDirOK(t, (Cmd[Params]{RunFunc: baseDirNoop[Params], PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
				if p.Input != want || p.Token != "secret\n" {
					t.Fatalf("hook values: %+v", p)
				}
				return nil
			}}).RunArgsE(tc.args))
		})
	}
}

func TestBaseDir_OverlaysAndConfigPaths(t *testing.T) {
	type Path string
	type Group struct {
		Config string `configfile:"true" default:"nested.json"`
		Input  string `file:"true" default:"input"`
		Value  string `optional:"true"`
	}
	type Params struct {
		Dir     string      `basedir:"true"`
		Configs []Path      `configfile:"true" default:"[base.json,overlay.json]"`
		Last    baseDirText `configfile:"true" default:"absent.json" env:"BOA_TEST_LAST"`
		Group   Group
	}
	dir := t.TempDir()
	input := baseDirFile(t, dir, "input", "data")
	nested := baseDirFile(t, dir, "nested.json", `{"Value":"nested"}`)
	base := baseDirFile(t, dir, "base.json", `{"Last":"wrong.json","Group":{"Value":"base"}}`)
	overlay := baseDirFile(t, dir, "overlay.json", `{"Group":{"Value":"overlay"}}`)
	last := baseDirFile(t, t.TempDir(), "last.json", `{"Group":{"Input":"input"}}`)
	for _, source := range []string{"cli", "env"} {
		t.Run(source, func(t *testing.T) {
			args := []string{"--dir", dir, "--last", "path:" + last}
			if source == "env" {
				t.Setenv("BOA_TEST_LAST", "path:"+last)
				args = args[:2]
			}
			baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				if string(p.Last) != last || p.Group.Value != "overlay" || p.Group.Input != input || p.Group.Config != nested || !slices.Equal(p.Configs, []Path{Path(base), Path(overlay)}) {
					t.Fatalf("merged paths: %+v", p)
				}
				if !slices.Equal(ctx.WatchedConfigFiles(), []string{nested, base, overlay, last}) {
					t.Fatalf("watches: %v", ctx.WatchedConfigFiles())
				}
				data, err := ctx.DumpBytes(".json", nil)
				baseDirOK(t, err)
				var dump map[string]any
				baseDirOK(t, json.Unmarshal(data, &dump))
				if _, present := dump["Dir"]; present || dump["Group"].(map[string]any)["Input"] != input {
					t.Fatalf("dump: %s", data)
				}
			}}).RunArgsE(args))
		})
	}
}

func TestBaseDir_DirectoryLifecycle(t *testing.T) {
	type Params struct {
		Dir string `basedir:"required,autocreate"`
	}
	missing := filepath.Join(t.TempDir(), "parent", "data")
	for _, args := range [][]string{{"--help"}, {cobra.ShellCompRequestCmd, "--dir", ""}, {"completion", "bash"}} {
		cmd := (Cmd[Params]{Use: "test", Params: &Params{Dir: missing}, RunFunc: baseDirNoop[Params]}).ToCobra()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		_ = cmd.Execute()
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("help/completion created directory: %v", err)
		}
	}
	if err := (Cmd[Params]{RawArgs: []string{"--dir", missing}}).Validate(); !IsUserInputError(err) {
		t.Fatalf("required Validate: %v", err)
	}
	baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, _ *Params, _ *cobra.Command, _ []string) {
		info, err := os.Stat(missing)
		baseDirOK(t, err)
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("permissions: %v", info.Mode())
		}
		baseDirOK(t, os.Remove(missing))
		if _, err := Reload[Params](ctx); !IsUserInputError(err) {
			t.Fatalf("required reload: %v", err)
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("reload created directory: %v", err)
		}
	}}).RunArgsE([]string{"--dir", missing}))
	type Auto struct {
		Dir    string `basedir:"autocreate"`
		Config string `configfile:"true" optional:"true"`
	}
	baseDirOK(t, (Cmd[Auto]{RawArgs: []string{"--dir", missing}}).Validate())
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("Validate created directory: %v", err)
	}
	if err := (Cmd[Auto]{RunFunc: baseDirNoop[Auto]}).RunArgsE([]string{"--dir", missing, "--config", "absent.json"}); !IsUserInputError(err) {
		t.Fatalf("config failure: %v", err)
	}
	baseDirOK(t, os.Chmod(missing, 0o755)) // Creation survives config failure; existing modes must survive execution.
	baseDirOK(t, (Cmd[Auto]{RunFunc: baseDirNoop[Auto]}).RunArgsE([]string{"--dir", missing}))
	info, err := os.Stat(missing)
	baseDirOK(t, err)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("changed permissions: %v", info.Mode())
	}
	file := baseDirFile(t, missing, "file", "data")
	for _, path := range []string{file, filepath.Join(file, "child")} {
		if err := (Cmd[Auto]{RunFunc: baseDirNoop[Auto]}).RunArgsE([]string{"--dir", path}); !IsUserInputError(err) {
			t.Fatalf("creation error for %s: %v", path, err)
		}
	}
	type Required struct {
		Dir string `basedir:"required"`
	}
	if err := (Cmd[Required]{RawArgs: []string{"--dir", file}}).Validate(); !IsUserInputError(err) {
		t.Fatalf("file accepted as directory: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	baseDirOK(t, os.Symlink(missing, link))
	baseDirOK(t, (Cmd[Required]{RawArgs: []string{"--dir", link}}).Validate())
}

func TestBaseDir_OptionalGroupsAndHooks(t *testing.T) {
	type Group struct {
		Dir   string `basedir:"true"`
		Input string `file:"true" default:"absent"`
	}
	type Params struct {
		Group  *Group
		Config string `configfile:"true" optional:"true"`
	}
	var p Params
	baseDirOK(t, (Cmd[Params]{Params: &p, RawArgs: []string{}}).Validate())
	if p.Group != nil {
		t.Fatalf("unused group survived: %+v", p)
	}
	config := baseDirFile(t, t.TempDir(), "config.json", `{"Group":{}}`)
	if err := (Cmd[Params]{RawArgs: []string{"--config", config}}).Validate(); !IsUserInputError(err) {
		t.Fatalf("active group accepted missing base: %v", err)
	}
	type Hooks struct {
		Dir   string `basedir:"true" default:"."`
		Input string `file:"true" default:"input"`
	}
	dir := t.TempDir()
	t.Chdir(dir)
	baseDirFile(t, dir, "input", "data")
	changed := baseDirFile(t, dir, "changed", "data")
	for _, path := range []string{".", "elsewhere"} {
		p := Hooks{}
		err := (Cmd[Hooks]{Params: &p, RunFunc: baseDirNoop[Hooks], PreValidateFunc: func(p *Hooks, _ *cobra.Command, _ []string) error {
			p.Dir = path
			p.Input = "changed"
			return nil
		}}).RunArgsE(nil)
		if path == "elsewhere" {
			if !IsUserInputError(err) || !strings.Contains(err.Error(), "cannot change") {
				t.Fatalf("changed base accepted: %v", err)
			}
		} else {
			baseDirOK(t, err)
			if p.Dir != dir || p.Input != changed {
				t.Fatalf("unresolved hook paths: %+v", p)
			}
		}
	}
	type Missing struct {
		Dir    string `basedir:"autocreate"`
		Config string `configfile:"true" default:"absent.json"`
	}
	if err := (Cmd[Missing]{RawArgs: []string{}}).Validate(); err == nil || !strings.Contains(err.Error(), "missing required param 'dir'") {
		t.Fatalf("required base must precede config: %v", err)
	}
}

func TestBaseDir_Reload(t *testing.T) {
	type Params struct {
		Dir     string   `basedir:"true" env:"BOA_TEST_RELOAD_BASE"`
		Configs []string `configfile:"true" default:"[config.json]"`
		Input   string   `file:"true"`
	}
	for _, source := range []string{"cli", "env"} {
		t.Run(source, func(t *testing.T) {
			origin := t.TempDir()
			t.Chdir(origin)
			for _, name := range []string{"first", "second"} {
				dir := filepath.Join(origin, name)
				baseDirOK(t, os.Mkdir(dir, 0o700))
				baseDirFile(t, dir, "input", "data")
				baseDirFile(t, dir, "config.json", `{"Input":"input"}`)
			}
			t.Setenv("BOA_TEST_RELOAD_BASE", "first")
			var args []string
			want := filepath.Join(origin, "second")
			if source == "cli" {
				args = []string{"--dir", "first"}
				want = filepath.Join(origin, "first")
			}
			baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				baseDirOK(t, os.Chdir(t.TempDir()))
				t.Setenv("BOA_TEST_RELOAD_BASE", "second")
				fresh, err := Reload[Params](ctx)
				baseDirOK(t, err)
				watched := ctx.WatchedConfigFiles()
				if fresh.Dir != want || fresh.Input != filepath.Join(want, "input") || !slices.Equal(fresh.Configs, []string{filepath.Join(want, "config.json")}) || !slices.Equal(watched, fresh.Configs) {
					t.Fatalf("reload: %+v watches=%v", fresh, watched)
				}
				baseDirFile(t, want, "config.json", `{"Input":"missing"}`)
				if fresh, err := Reload[Params](ctx); err == nil || fresh != nil {
					t.Fatalf("invalid reload: %+v, %v", fresh, err)
				}
				if p.Dir != filepath.Join(origin, "first") || !slices.Equal(watched, ctx.WatchedConfigFiles()) {
					t.Fatal("failed reload changed previous state")
				}
			}}).RunArgsE(args))
		})
	}
}

func TestBaseDir_Inheritance(t *testing.T) {
	type Root struct {
		Dir     string `basedir:"true" env:"BOA_TEST_PARENT_BASE"`
		Verbose bool   `persistent:"true"`
	}
	type Leaf struct {
		Dir   string `name:"child-dir" basedir:"true" default:"."`
		Input string `file:"true" default:"input"`
	}
	for _, mode := range []string{"inherited", "override", "disabled", "local"} {
		for _, traversal := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "composed", true: "traversed"}[traversal], func(t *testing.T) {
				previous := cobra.EnableTraverseRunHooks
				cobra.EnableTraverseRunHooks = traversal
				t.Cleanup(func() { cobra.EnableTraverseRunHooks = previous })
				origin := t.TempDir()
				t.Chdir(origin)
				baseDirFile(t, origin, "input", "data")
				parent := t.TempDir()
				baseDirFile(t, parent, "input", "data")
				t.Setenv("BOA_TEST_PARENT_BASE", parent)
				want := filepath.Join(parent, "input")
				if mode == "override" || mode == "disabled" {
					want = filepath.Join(origin, "input")
				}
				if mode == "local" {
					want = "input"
				}
				leaf := (Cmd[Leaf]{Use: "leaf", InitFuncCtx: func(ctx *HookContext, p *Leaf, _ *cobra.Command) error {
					Param(ctx, &p.Dir).SetBaseDir(mode == "override")
					return nil
				}, RunFuncCtx: func(ctx *HookContext, p *Leaf, _ *cobra.Command, _ []string) {
					if p.Input != want {
						t.Fatalf("path=%s want=%s", p.Input, want)
					}
					t.Setenv("BOA_TEST_PARENT_BASE", "missing")
					fresh, err := Reload[Leaf](ctx)
					baseDirOK(t, err)
					if fresh.Input != want {
						t.Fatalf("inherited reload: %+v", fresh)
					}
				}}).ToCobra()
				middle := &cobra.Command{Use: "middle"}
				middle.AddCommand(leaf)
				root := (Cmd[Root]{Use: "root", SubCmds: []*cobra.Command{middle}, InitFuncCtx: func(ctx *HookContext, p *Root, _ *cobra.Command) error {
					Param(ctx, &p.Dir).SetPersistent(mode != "local")
					Param(ctx, &p.Dir).SetIsEnabledFn(func() bool { return mode != "disabled" })
					return nil
				}}).ToCobra()
				root.SetArgs([]string{"middle", "leaf"})
				baseDirOK(t, root.Execute())
			})
		}
	}
}

func TestBaseDir_OptionalDisabledAndIgnored(t *testing.T) {
	type Params struct {
		Dir   *string
		Input string `file:"true" default:"input"`
		Group *struct {
			Input string `file:"true" default:"absent"`
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	input := baseDirFile(t, dir, "input", "data")
	for _, mode := range []string{"enabled", "disabled", "ignored", "removed", "code", "empty"} {
		p := Params{}
		if mode == "code" {
			p.Dir = &dir
		}
		var args []string
		if mode == "empty" {
			args = []string{"--dir="}
		}
		baseDirOK(t, (Cmd[Params]{Params: &p, RunFunc: baseDirNoop[Params], InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
			base := Param(ctx, &p.Dir)
			base.SetBaseDirAutoCreate(true)
			base.SetIsEnabledFn(func() bool { return mode != "disabled" })
			base.SetIgnored(mode == "ignored")
			if mode == "removed" {
				base.SetBaseDir(false)
			}
			return nil
		}}).RunArgsE(args))
		want := input
		if mode == "removed" || mode == "ignored" {
			want = "input"
		}
		if p.Input != want || p.Group != nil || (p.Dir != nil) != (mode == "code" || mode == "empty") {
			t.Fatalf("%s: %+v", mode, p)
		}
	}
	type Ignored struct {
		Dir string `basedir:"true" boa:"ignore"`
	}
	var p Ignored
	baseDirOK(t, LoadConfigBytes([]byte(`{"Dir":"data"}`), ".json", &p, nil))
	if p.Dir != "data" {
		t.Fatalf("ignored provider: %+v", p)
	}
}

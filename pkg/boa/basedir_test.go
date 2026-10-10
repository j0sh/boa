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
	for _, tag := range []string{`basedir:"invalid"`, `basedir:"true" file:"true"`, `basedir:"true" file:"optional"`, `basedir:"true" configfile:"true"`, `basedir:"true" configfile:"optional"`, `basedir:"true" secret:"true"`, `basedir:"true" secretfor:"Token"`} {
		typ := reflect.StructOf([]reflect.StructField{
			{Name: "Dir", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(tag)},
			{Name: "Token", Type: reflect.TypeFor[string](), Tag: `secret:"true" optional:"true"`},
		})
		if _, err := (command{Params: reflect.New(typ).Interface()}).ToCobraE(); err == nil || IsUserInputError(err) || !strings.Contains(err.Error(), "basedir") || !strings.Contains(err.Error(), "dir") && !strings.Contains(err.Error(), "Dir") {
			t.Fatalf("expected basedir declaration error for Dir with %s: %v", tag, err)
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
		if !base.IsBaseDir() || base.IsNoConfig() {
			t.Fatal("provider must accept config input")
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
	missingConfig := baseDirFile(t, dir, "missing-config.json", `{"Input":"config-missing"}`)
	absolute := baseDirFile(t, t.TempDir(), "absolute", "data")
	for _, tc := range []struct {
		name, env, base, initial string
		args                     []string
		optional                 bool
	}{
		{"default", "", "", "", []string{"--config="}, false},
		{"config", "", dir, "", nil, false},
		{"env", "env", dir, "", nil, false},
		{"cli", "env", "wrong", "", []string{"--dir", dir, "--input", "cli"}, false},
		{absolute, "", "missing", "", []string{"--input", absolute, "--config=", "--token-file", filepath.Join(dir, "token")}, false},
		{"default-missing", "", dir, "", []string{"--config="}, true},
		{"config-missing", "", dir, "", []string{"--config", missingConfig}, true},
		{"env-missing", "env-missing", dir, "", nil, true},
		{"cli-missing", "env-missing", "wrong", "", []string{"--dir", dir, "--input", "cli-missing"}, true},
		{"application-missing", "", dir, "application-missing", []string{"--config="}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_TEST_BASE", tc.base)
			t.Setenv("BOA_TEST_INPUT", tc.env)
			want := resolvePath(dir, tc.name)
			p := Params{Input: tc.initial}
			baseDirOK(t, (Cmd[Params]{Params: &p, RunFunc: baseDirNoop[Params], InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
				if tc.optional {
					Param(ctx, &p.Input).SetFileOptional(true)
					Param(ctx, &p.Input).SetDefault("default-missing")
				}
				return nil
			}, PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
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
		Config string `configfile:"optional" default:"nested.json"`
		Input  string `file:"true" default:"input"`
		Value  string `optional:"true"`
	}
	type Params struct {
		Dir     string      `basedir:"true"`
		Configs []Path      `configfile:"true,optional" default:"[base.json,missing.json,overlay.json]"`
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
			watched := []string{nested, base, overlay, last}
			if source == "env" {
				baseDirOK(t, os.Remove(nested))
				watched = watched[1:]
				t.Setenv("BOA_TEST_LAST", "path:"+last)
				args = args[:2]
			}
			baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				if string(p.Last) != last || p.Group.Value != "overlay" || p.Group.Input != input || p.Group.Config != nested || !slices.Equal(p.Configs, []Path{Path(base), Path(filepath.Join(dir, "missing.json")), Path(overlay)}) {
					t.Fatalf("merged paths: %+v", p)
				}
				if !slices.Equal(ctx.WatchedConfigFiles(), watched) {
					t.Fatalf("watches: %v", ctx.WatchedConfigFiles())
				}
				data, err := ctx.DumpBytes(".json", nil)
				baseDirOK(t, err)
				var dump map[string]any
				baseDirOK(t, json.Unmarshal(data, &dump))
				if dump["Dir"] != dir || dump["Group"].(map[string]any)["Input"] != input {
					t.Fatalf("dump: %s", data)
				}
			}}).RunArgsE(args))
		})
	}
	t.Run("strict sibling", func(t *testing.T) {
		err := (Cmd[Params]{RawArgs: []string{"--dir", dir, "--last", "missing.json"}}).Validate()
		assertUserInputError(t, err, os.ErrNotExist, "configfile last", filepath.Join(dir, "missing.json"))
	})
}

func TestBaseDir_DirectoryLifecycle(t *testing.T) {
	type Params struct {
		Dir    string `basedir:"required,autocreate"`
		Config string `configfile:"true" optional:"true"`
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
	data, err := json.Marshal(map[string]string{"Dir": missing})
	baseDirOK(t, err)
	config := baseDirFile(t, t.TempDir(), "config.json", string(data))
	for _, args := range [][]string{{"--dir", missing}, {"--config", config}} {
		if err := (Cmd[Params]{RawArgs: args}).Validate(); !IsUserInputError(err) {
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
		}}).RunArgsE(args))
	}
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
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("config failure created directory: %v", err)
	}
	baseDirOK(t, os.MkdirAll(missing, 0o755)) // Existing modes must survive execution.
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
	if err := (Cmd[Missing]{RawArgs: []string{}}).Validate(); err == nil || !strings.Contains(err.Error(), "configfile config") {
		t.Fatalf("config failure must precede required base: %v", err)
	}
}

func TestBaseDir_ClearedConfigGroup(t *testing.T) {
	type Group struct {
		Dir string `basedir:"required,autocreate"`
	}
	type Params struct {
		Configs []string `configfile:"true"`
		Group   *Group
		Output  string `basepath:"basedir" default:"output"`
	}
	origin := t.TempDir()
	t.Chdir(origin)
	first := baseDirFile(t, origin, "first.json", `{"Group":{"Dir":"stale"}}`)
	last := baseDirFile(t, origin, "last.json", `{"Group":null}`)
	for _, configs := range []string{last, first + "," + last} {
		var p Params
		cmd := Cmd[Params]{Params: &p, RawArgs: []string{"--configs", configs}, RunFunc: baseDirNoop[Params]}
		baseDirOK(t, cmd.Validate())
		baseDirOK(t, cmd.RunArgsE(cmd.RawArgs))
		if p.Group != nil || p.Output != filepath.Join(origin, "output") {
			t.Fatalf("cleared base: %+v", p)
		}
		if _, err := os.Stat(filepath.Join(origin, "stale")); !os.IsNotExist(err) {
			t.Fatalf("cleared base created directory: %v", err)
		}
	}
}

func TestBaseDir_Reload(t *testing.T) {
	type Params struct {
		Dir     string   `basedir:"true" default:"." env:"BOA_TEST_RELOAD_BASE"`
		Configs []string `configfile:"true" default:"[config.json]"`
		Input   string   `file:"true"`
	}
	for _, source := range []string{"cli", "env", "config"} {
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
			if source == "config" {
				t.Setenv("BOA_TEST_RELOAD_BASE", "")
				baseDirFile(t, origin, "config.json", `{"Dir":"first","Input":"input"}`)
			}
			var args []string
			want := filepath.Join(origin, "second")
			if source == "cli" {
				args = []string{"--dir", "first"}
				want = filepath.Join(origin, "first")
			}
			baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				baseDirOK(t, os.Chdir(t.TempDir()))
				configDir := want
				if source == "config" {
					configDir = origin
					baseDirFile(t, origin, "config.json", `{"Dir":"second","Input":"input"}`)
				} else {
					t.Setenv("BOA_TEST_RELOAD_BASE", "second")
				}
				fresh, err := Reload[Params](ctx)
				baseDirOK(t, err)
				watched := ctx.WatchedConfigFiles()
				if fresh.Dir != want || fresh.Input != filepath.Join(want, "input") || !slices.Equal(fresh.Configs, []string{filepath.Join(configDir, "config.json")}) || !slices.Equal(watched, fresh.Configs) {
					t.Fatalf("reload: %+v watches=%v", fresh, watched)
				}
				baseDirFile(t, configDir, "config.json", `{"Input":"missing"}`)
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
		Config  string `configfile:"true" persistent:"true" name:"root-config" optional:"true"`
	}
	type Leaf struct {
		Dir    string `name:"child-dir" basedir:"true" default:"."`
		Input  string `file:"true" default:"input"`
		Config string `configfile:"optional" default:"child.json"`
	}
	for _, mode := range []string{"config", "inherited", "override", "disabled", "local"} {
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
				args := []string{"middle", "leaf"}
				if mode == "config" {
					t.Setenv("BOA_TEST_PARENT_BASE", "")
					data, err := json.Marshal(map[string]string{"Dir": parent})
					baseDirOK(t, err)
					config := baseDirFile(t, origin, "root.json", string(data))
					baseDirFile(t, parent, "child.json", `{"Input":"input"}`)
					args = append([]string{"--root-config", config}, args...)
				}
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
					if p.Input != want || mode == "config" && p.Config != filepath.Join(parent, "child.json") {
						t.Fatalf("paths=%+v want=%s", p, want)
					}
					if mode != "local" {
						baseDirOK(t, os.Chdir(t.TempDir()))
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
				root.SetArgs(args)
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

func TestBaseDir_ConfigPrecedenceAndStableConfigPaths(t *testing.T) {
	type Group struct {
		Dir    string `basedir:"true" default:"initial" env:"BOA_CONFIG_BASE"`
		Config string `configfile:"optional" default:"nested.json"`
	}
	type Params struct {
		Group     Group
		Config    string `configfile:"optional" default:"root.json"`
		Next      string `configfile:"true" optional:"true"`
		Input     string `file:"true" default:"input"`
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token" default:"token"`
	}
	for _, tc := range []struct {
		name, nested, root, env, cli, want string
	}{
		{name: "default", want: "initial"},
		{name: "nested", nested: `{"Dir":"nested"}`, want: "nested"},
		{name: "root", nested: `{"Dir":"nested"}`, root: `{"Group":{"Dir":"root"},"Next":"next.json"}`, want: "root"},
		{name: "empty config", root: `{"Group":{"Dir":""}}`, want: "."},
		{name: "environment", root: `{"Group":{"Dir":"root"}}`, env: "env", want: "env"},
		{name: "CLI", root: `{"Group":{"Dir":"root"}}`, env: "env", cli: "cli", want: "cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := t.TempDir()
			t.Chdir(origin)
			t.Setenv("GROUP_BOA_CONFIG_BASE", tc.env)
			initialBase := filepath.Join(origin, "initial")
			if tc.env != "" {
				initialBase = filepath.Join(origin, tc.env)
			}
			var args []string
			if tc.cli != "" {
				args = []string{"--group-dir", tc.cli}
				initialBase = filepath.Join(origin, tc.cli)
			}
			final := filepath.Join(origin, tc.want)
			baseDirOK(t, os.MkdirAll(initialBase, 0o700))
			baseDirOK(t, os.MkdirAll(final, 0o700))
			input := baseDirFile(t, final, "input", "data")
			baseDirFile(t, final, "token", "secret")
			if tc.nested != "" {
				baseDirFile(t, initialBase, "nested.json", tc.nested)
			}
			if tc.root != "" {
				baseDirFile(t, initialBase, "root.json", tc.root)
				baseDirFile(t, initialBase, "next.json", `{}`)
			}
			baseDirOK(t, (Cmd[Params]{RunFunc: baseDirNoop[Params], PreValidateFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
				if p.Group.Dir != final || p.Input != input || p.Token != "secret" || p.Config != filepath.Join(initialBase, "root.json") || p.Group.Config != filepath.Join(initialBase, "nested.json") {
					t.Fatalf("merged paths: %+v", p)
				}
				if ctx.HasInput(&p.Group.Dir) != (tc.name != "default") {
					t.Fatal("base input presence lost")
				}
				if strings.Contains(tc.root, "Next") && p.Next != filepath.Join(initialBase, "next.json") {
					t.Fatalf("config selection followed final base: %s", p.Next)
				}
				return nil
			}}).RunArgsE(args))
		})
	}
}

func TestBaseDir_ConfigHooksAndReload(t *testing.T) {
	type Params struct {
		Dir     string `basedir:"true" optional:"true"`
		Config  string `configfile:"true" default:"config.json"`
		Network string `default:"default" env:"BOA_CONFIG_NETWORK"`
		Count   int    `default:"7"`
	}
	origin := t.TempDir()
	t.Chdir(origin)
	t.Setenv("BOA_CONFIG_NETWORK", "first")
	for _, name := range []string{"first", "second"} {
		baseDirOK(t, os.Mkdir(filepath.Join(origin, name), 0o700))
		baseDirFile(t, filepath.Join(origin, name), "config.json", `{"Network":"config","Count":0}`)
	}
	var phases []string
	cmd := Cmd[Params]{PreConfigFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
		phases = append(phases, "pre")
		if !ctx.HasInput(&p.Network) || ctx.HasInput(&p.Dir) || ctx.HasInput(&p.Count) {
			t.Fatal("pre-config presence")
		}
		p.Dir = p.Network
		return nil
	}, PostConfigFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
		phases = append(phases, "post")
		if !ctx.HasInput(&p.Count) || p.Count != 0 || ctx.HasInput(&p.Dir) || !ctx.HasValue(&p.Dir) {
			t.Fatalf("post-config presence or zero: %+v", p)
		}
		p.Dir = "data-" + p.Network
		return nil
	}, PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
		phases = append(phases, "validate")
		return nil
	}, RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
		if p.Dir != filepath.Join(origin, "data-first") {
			t.Fatalf("derived final base: %+v", p)
		}
		baseDirOK(t, os.Chdir(t.TempDir()))
		t.Setenv("BOA_CONFIG_NETWORK", "second")
		fresh, err := Reload[Params](ctx)
		baseDirOK(t, err)
		if fresh.Dir != filepath.Join(origin, "data-second") || fresh.Config != filepath.Join(origin, "second", "config.json") || p.Dir != filepath.Join(origin, "data-first") {
			t.Fatalf("reload: %+v previous: %+v", fresh, p)
		}
		watches := ctx.WatchedConfigFiles()
		baseDirFile(t, filepath.Join(origin, "second"), "config.json", `{`)
		if fresh, err := Reload[Params](ctx); err == nil || fresh != nil || !slices.Equal(watches, ctx.WatchedConfigFiles()) {
			t.Fatalf("failed reload: %+v %v", fresh, err)
		}
	}}
	baseDirOK(t, cmd.Validate())
	baseDirOK(t, cmd.RunArgsE(nil))
	if !slices.Equal(phases, []string{"pre", "post", "validate", "pre", "post", "validate", "pre", "post", "validate", "pre"}) {
		t.Fatalf("hook phases: %v", phases)
	}
}

package boa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBasePath_SourcesAndOverlayOrigins(t *testing.T) {
	type Group struct {
		Config    string   `configfile:"true" optional:"true" basepath:"source"`
		Input     *string  `file:"true" basepath:"source" default:"input" env:"INPUT"`
		Outputs   []string `basepath:"source" default:"[out,]"`
		Token     string   `secret:"true"`
		TokenFile string   `secretfor:"Token" basepath:"source" default:"token"`
	}
	type Params struct {
		Dir     string   `basedir:"true" basepath:"source" default:"default"`
		Configs []string `configfile:"true" optional:"true" basepath:"source"`
		Storage string   `basepath:"basedir" default:"database"`
		Group   Group
	}
	for _, source := range []string{"default", "nested", "root", "overlay", "absent overlay", "null overlay", "mixed-case overlay", "CLI", "env"} {
		t.Run(source, func(t *testing.T) {
			origin := t.TempDir()
			t.Chdir(origin)
			for _, dir := range []string{"default", "nested", "root", "overlay", "overlay/data", "root/data"} {
				baseDirOK(t, os.MkdirAll(filepath.Join(origin, dir), 0o700))
			}
			for _, dir := range []string{"", "default", "nested", "root", "overlay"} {
				baseDirFile(t, filepath.Join(origin, dir), "input", "data")
				baseDirFile(t, filepath.Join(origin, dir), "token", "secret")
			}
			baseDirFile(t, filepath.Join(origin, "nested"), "config.json", `{"Input":"input","TokenFile":"token","Outputs":["out",""]}`)
			baseDirFile(t, filepath.Join(origin, "root"), "config.json", `{"Dir":"data","Group":{"Input":"input","TokenFile":"token","Outputs":["out",""]}}`)
			overlay := `{"Dir":"data"}`
			if source == "overlay" {
				overlay = `{"Dir":"data","Group":{"Input":"input","TokenFile":"token","Outputs":["out",""]}}`
			}
			if source == "null overlay" {
				overlay = `{"Dir":"data","Group":{"TokenFile":null}}`
			}
			if source == "mixed-case overlay" {
				overlay = `{"Dir":"data","Group":{"TokenFile":null,"tokenfile":"token"}}`
			}
			baseDirFile(t, filepath.Join(origin, "overlay"), "config.json", overlay)
			var args []string
			base, inputBase := filepath.Join(origin, "default"), filepath.Join(origin, "default")
			if source != "default" {
				args = []string{"--group-config", "nested/config.json"}
				inputBase = filepath.Join(origin, "nested")
			}
			if source != "default" && source != "nested" {
				args = append(args, "--configs", "root/config.json")
				base, inputBase = filepath.Join(origin, "root", "data"), filepath.Join(origin, "root")
			}
			if strings.Contains(source, "overlay") {
				args = append(args, "--configs", "overlay/config.json")
				base = filepath.Join(origin, "overlay", "data")
				if source == "overlay" {
					inputBase = filepath.Join(origin, "overlay")
				}
			}
			tokenBase := inputBase
			if source == "mixed-case overlay" {
				tokenBase = filepath.Join(origin, "overlay")
			}
			wantInputBase := inputBase
			t.Setenv("GROUP_INPUT", "")
			if source == "CLI" {
				args = append(args, "--group-input", "input")
				wantInputBase = origin
			}
			if source == "env" {
				t.Setenv("GROUP_INPUT", "input")
				wantInputBase = origin
			}
			baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				if p.Dir != base || p.Storage != filepath.Join(base, "database") || p.Group.Input == nil || *p.Group.Input != filepath.Join(wantInputBase, "input") || p.Group.TokenFile != filepath.Join(tokenBase, "token") || p.Group.Token != "secret" {
					t.Fatalf("source paths: %+v group=%+v", p, p.Group)
				}
				if !slices.Equal(p.Group.Outputs, []string{filepath.Join(inputBase, "out"), ""}) {
					t.Fatalf("slice paths: %v", p.Group.Outputs)
				}
				if ctx.HasInput(&p.Group.Input) != (source != "default") || ctx.HasInput(&p.Storage) || ctx.HasInput(&p.Group.Token) {
					t.Fatal("normalization changed source presence")
				}
				if Param(ctx, &p.Group.Input).Parameter.(*paramMeta).wasSetOnCli() != (source == "CLI") {
					t.Fatal("normalization changed CLI presence")
				}
			}}).RunArgsE(args))
		})
	}
}

func TestBasePath_ReloadAndSourceBase(t *testing.T) {
	type Params struct {
		Dir    string `basedir:"true" basepath:"source" default:"." env:"BOA_PATH_BASE"`
		Config string `configfile:"true" basepath:"source"`
		Input  string `file:"true" basepath:"source" env:"BOA_PATH_INPUT"`
	}
	origin := t.TempDir()
	t.Chdir(origin)
	baseDirOK(t, os.Mkdir(filepath.Join(origin, "configs"), 0o700))
	config := baseDirFile(t, filepath.Join(origin, "configs"), "config.json", `{"Dir":"data","Input":"input"}`)
	input := baseDirFile(t, filepath.Dir(config), "input", "data")
	envInput := baseDirFile(t, origin, "env-input", "data")
	t.Setenv("BOA_PATH_INPUT", "")
	t.Setenv("BOA_PATH_BASE", "")
	baseDirOK(t, (Cmd[Params]{PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
		if p.Dir == filepath.Join(filepath.Dir(config), "data") {
			p.Dir = "data" // An equivalent config-relative value preserves the frozen base.
		}
		return nil
	}, RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
		if p.Dir != filepath.Join(filepath.Dir(config), "data") || p.Input != input {
			t.Fatalf("config-relative base: %+v", p)
		}
		baseDirOK(t, os.Chdir(t.TempDir()))
		t.Setenv("BOA_PATH_BASE", "env-base")
		t.Setenv("BOA_PATH_INPUT", "env-input")
		fresh, err := Reload[Params](ctx)
		baseDirOK(t, err)
		if fresh.Config != config || fresh.Dir != filepath.Join(origin, "env-base") || fresh.Input != envInput || !slices.Equal(ctx.WatchedConfigFiles(), []string{config}) {
			t.Fatalf("source reload: %+v", fresh)
		}
	}}).RunArgsE([]string{"--config", "configs/config.json"}))
}

func TestBasePath_ConfigSelectedByConfig(t *testing.T) {
	type Params struct {
		Dir   string `basedir:"true" default:"unused"`
		First string `configfile:"true" basepath:"source"`
		Next  string `configfile:"true" basepath:"source" optional:"true"`
		Input string `basepath:"source"`
	}
	origin := t.TempDir()
	t.Chdir(origin)
	dir := t.TempDir()
	first := baseDirFile(t, dir, "first.json", `{"Dir":"final","Next":"next.json"}`)
	next := baseDirFile(t, dir, "next.json", `{"Input":"input"}`)
	baseDirOK(t, (Cmd[Params]{RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
		if p.Next != next || p.Dir != filepath.Join(origin, "final") || p.Input != filepath.Join(dir, "input") || !slices.Equal(ctx.WatchedConfigFiles(), []string{first, next}) {
			t.Fatalf("config-selected path: %+v watches=%v", p, ctx.WatchedConfigFiles())
		}
	}}).RunArgsE([]string{"--first", first}))
}

func TestBasePath_ProgrammaticAndInvalidDeclarations(t *testing.T) {
	type Params struct {
		Config string `configfile:"true"`
		Path   string
	}
	origin := t.TempDir()
	t.Chdir(origin)
	dir := t.TempDir()
	config := baseDirFile(t, dir, "config.json", `{"Path":"out"}`)
	var p Params
	cmd := Cmd[Params]{Params: &p, RawArgs: []string{"--config", config}, InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
		Param(ctx, &p.Path).SetBasePath(BasePathSource)
		return nil
	}}
	baseDirOK(t, cmd.Validate())
	if p.Path != filepath.Join(dir, "out") {
		t.Fatalf("programmatic source path: %+v", p)
	}
	for _, tag := range []string{`basepath:"invalid"`, `basepath:""`, `basepath:"source"`} {
		typ := reflect.StructOf([]reflect.StructField{{Name: "Path", Type: reflect.TypeFor[int](), Tag: reflect.StructTag(tag)}})
		_, err := (command{Params: reflect.New(typ).Interface()}).ToCobraE()
		if err == nil || !strings.Contains(err.Error(), "basepath") {
			t.Fatalf("invalid declaration %s: %v", tag, err)
		}
	}
}

func TestHasInput_ExplicitAndGeneratedValues(t *testing.T) {
	type Params struct {
		Config    string `configfile:"true" optional:"true"`
		Value     string `default:"default" env:"BOA_INPUT_VALUE"`
		Generated string `optional:"true"`
	}
	for _, source := range []string{"default", "CLI default", "CLI empty", "env default", "env empty", "config default", "config empty"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_INPUT_VALUE", "")
			if source != "env empty" {
				baseDirOK(t, os.Unsetenv("BOA_INPUT_VALUE"))
			}
			want := "default"
			var args []string
			switch source {
			case "CLI default":
				args = []string{"--value", "default"}
			case "CLI empty":
				args, want = []string{"--value="}, ""
			case "env default":
				t.Setenv("BOA_INPUT_VALUE", "default")
			case "config default", "config empty":
				if source == "config empty" {
					want = ""
				}
				config := baseDirFile(t, t.TempDir(), "config.json", `{"Value":"`+want+`"}`)
				args = []string{"--config", config}
			}
			baseDirOK(t, (Cmd[Params]{RawArgs: args, ConfigFormat: UniversalConfigFormat(json.Unmarshal), PostConfigFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
				if ctx.HasInput(&p.Value) != (source != "default" && source != "env empty") || !ctx.HasValue(&p.Value) || p.Value != want {
					t.Fatalf("input presence or value: %+v", p)
				}
				p.Generated = "generated"
				return nil
			}, PreValidateFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
				if ctx.HasInput(&p.Generated) || !ctx.HasValue(&p.Generated) {
					t.Fatal("application value counted as input")
				}
				return nil
			}}).Validate())
		})
	}
}

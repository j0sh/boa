package boa

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNoConfig_AutomaticRejectsBeforeDecode(t *testing.T) {
	const ext = ".noconfig-before-decode"
	targetCalls := 0
	registerFormatCleanup(t, ext, ConfigFormat{
		Unmarshal: func(data []byte, target any) error {
			targetCalls++
			return json.Unmarshal(data, target)
		},
		KeyTree: jsonKeyTree,
	})

	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Allowed    string `optional:"true"`
		Secret     string `boa:"noconfig" noconfig-before-decode:"token" optional:"true"`
	}
	params := &Params{Allowed: "original", Secret: "original"}
	path := filepath.Join(t.TempDir(), "config"+ext)
	if err := os.WriteFile(path, []byte(`{"Allowed":"changed","token":"leaked"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := (Cmd[Params]{
		Use:     "test",
		Params:  params,
		RunFunc: func(*Params, *cobra.Command, []string) {},
	}).RunArgsE([]string{"--config-file", path})
	if err == nil {
		t.Fatal("expected noconfig error")
	}
	if !IsUserInputError(err) {
		t.Fatalf("expected user-input error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), `config key "token"`) || !strings.Contains(err.Error(), "field Secret") {
		t.Fatalf("error should identify raw key and Go field: %v", err)
	}
	if targetCalls != 0 {
		t.Fatalf("target decoder called %d times; rejection must happen before decode", targetCalls)
	}
	if params.Allowed != "original" || params.Secret != "original" {
		t.Fatalf("destination mutated before rejection: %+v", params)
	}
}

func TestNoConfig_OtherSourcesAndValidationStillWork(t *testing.T) {
	t.Run("environment-only recipe with unrelated config", func(t *testing.T) {
		t.Setenv("BOA_NOCONFIG_TOKEN", "from-env")
		type Params struct {
			ConfigFile string `configfile:"true" optional:"true"`
			Name       string `optional:"true"`
			Token      string `boa:"noflag,noconfig" env:"BOA_NOCONFIG_TOKEN" min:"4"`
		}
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"Name":"from-config"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var got Params
		err := (Cmd[Params]{
			Use: "test",
			RunFunc: func(p *Params, _ *cobra.Command, _ []string) {
				got = *p
			},
		}).RunArgsE([]string{"--config-file", path})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "from-config" || got.Token != "from-env" {
			t.Fatalf("unexpected resolved params: %+v", got)
		}
		if err := os.WriteFile(path, []byte(`{"Token":"from-config"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		err = (Cmd[Params]{
			Use:     "test",
			RunFunc: func(*Params, *cobra.Command, []string) {},
		}).RunArgsE([]string{"--config-file", path})
		if err == nil || !strings.Contains(err.Error(), `config key "Token"`) {
			t.Fatalf("environment value must not excuse a forbidden config key: %v", err)
		}
	})

	for _, tc := range []struct {
		name string
		env  string
		args []string
		want string
	}{
		{name: "flag", env: "from-env", args: []string{"--token", "from-cli"}, want: "from-cli"},
		{name: "environment", env: "from-env", want: "from-env"},
		{name: "default", want: "from-default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_NOCONFIG_SOURCE", tc.env)
			type Params struct {
				Token string `boa:"noconfig" env:"BOA_NOCONFIG_SOURCE" default:"from-default" min:"4"`
			}
			var got string
			err := (Cmd[Params]{
				Use: "test",
				RunFunc: func(p *Params, _ *cobra.Command, _ []string) {
					got = p.Token
				},
			}).RunArgsE(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Token = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("validation", func(t *testing.T) {
		t.Setenv("BOA_NOCONFIG_VALIDATE", "x")
		type Params struct {
			Token string `boa:"noflag,noconfig" env:"BOA_NOCONFIG_VALIDATE" min:"4"`
		}
		err := (Cmd[Params]{
			Use:     "test",
			RunFunc: func(*Params, *cobra.Command, []string) {},
		}).RunArgsE(nil)
		if err == nil || !strings.Contains(err.Error(), "below min") {
			t.Fatalf("expected ordinary validation error, got %v", err)
		}
	})
}

func TestNoConfig_LoadHelpersAndFormatPaths(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		type Params struct {
			Visible string
			Secret  string `boa:"noconfig" json:"secret_value"`
		}
		p := Params{Visible: "original", Secret: "original"}
		err := LoadConfigBytes([]byte(`{"Visible":"changed","secret_value":"bad"}`), ".json", &p, nil)
		if err == nil || !strings.Contains(err.Error(), `config key "secret_value"`) {
			t.Fatalf("expected renamed-key rejection, got %v", err)
		}
		if p.Visible != "original" || p.Secret != "original" {
			t.Fatalf("LoadConfigBytes mutated target: %+v", p)
		}
	})

	t.Run("file and overlay chain", func(t *testing.T) {
		type Params struct {
			Visible string
			Secret  string `boa:"noconfig"`
		}
		dir := t.TempDir()
		base := filepath.Join(dir, "base.json")
		bad := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(base, []byte(`{"Visible":"base"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bad, []byte(`{"Visible":"bad","Secret":"leaked"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		var p Params
		err := LoadConfigFiles([]string{base, bad}, &p, nil)
		if err == nil || !strings.Contains(err.Error(), `config key "Secret"`) {
			t.Fatalf("expected overlay rejection, got %v", err)
		}
		if p.Visible != "base" || p.Secret != "" {
			t.Fatalf("rejecting overlay should not decode that file: %+v", p)
		}
	})

	t.Run("nested and anonymous", func(t *testing.T) {
		type Embedded struct {
			Flat string `boa:"noconfig" json:"flat_secret"`
		}
		type Nested struct {
			Token string `boa:"noconfig" json:"token"`
		}
		type Params struct {
			Embedded
			Auth *Nested `json:"auth"`
		}
		for _, tc := range []struct {
			data, key, field string
		}{
			{data: `{"flat_secret":"bad"}`, key: "flat_secret", field: "Embedded.Flat"},
			{data: `{"auth":{"token":"bad"}}`, key: "auth.token", field: "Auth.Token"},
		} {
			var p Params
			err := LoadConfigBytes([]byte(tc.data), ".json", &p, nil)
			if err == nil || !strings.Contains(err.Error(), `config key "`+tc.key+`"`) || !strings.Contains(err.Error(), "field "+tc.field) {
				t.Fatalf("expected %s/%s rejection, got %v", tc.key, tc.field, err)
			}
		}
	})

	t.Run("registered custom format", func(t *testing.T) {
		const ext = ".noconfig-custom"
		registerFormatCleanup(t, ext, ConfigFormat{
			Unmarshal: json.Unmarshal,
			KeyTree:   jsonKeyTree,
		})
		type Params struct {
			Secret string `boa:"noconfig" noconfig-custom:"secret_name"`
		}
		var p Params
		err := LoadConfigBytes([]byte(`{"secret_name":"bad"}`), ext, &p, nil)
		if err == nil || !strings.Contains(err.Error(), `config key "secret_name"`) {
			t.Fatalf("expected custom-format rejection, got %v", err)
		}
	})

	t.Run("explicit decoder supplies key probe", func(t *testing.T) {
		type Params struct {
			Secret string `boa:"noconfig"`
		}
		mapCalls, targetCalls := 0, 0
		decode := func(data []byte, target any) error {
			if _, ok := target.(*map[string]any); ok {
				mapCalls++
			} else {
				targetCalls++
			}
			return json.Unmarshal(data, target)
		}
		var p Params
		err := LoadConfigBytes([]byte(`{"Secret":"bad"}`), ".json", &p, decode)
		if err == nil || !strings.Contains(err.Error(), `config key "Secret"`) {
			t.Fatalf("expected explicit-decoder rejection, got %v", err)
		}
		if mapCalls != 1 || targetCalls != 0 {
			t.Fatalf("map calls=%d target calls=%d, want 1/0", mapCalls, targetCalls)
		}
	})
}

func TestNoConfig_ProgrammaticNestedPolicy(t *testing.T) {
	type Nested struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `optional:"true"`
	}
	type Params struct {
		Nested Nested
	}
	path := filepath.Join(t.TempDir(), "nested.json")
	if err := os.WriteFile(path, []byte(`{"Secret":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := (Cmd[Params]{
		Use:     "test",
		RunFunc: func(*Params, *cobra.Command, []string) {},
		InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
			Param(ctx, &p.Nested.Secret).SetNoConfig(true)
			if !Param(ctx, &p.Nested.Secret).IsNoConfig() {
				t.Fatal("SetNoConfig did not update metadata")
			}
			return nil
		},
	}).RunArgsE([]string{"--nested-config-file", path})
	if err == nil || !strings.Contains(err.Error(), "field Secret") {
		t.Fatalf("expected programmatic nested rejection, got %v", err)
	}
}

func TestNoConfig_FailsClosedWithoutUsableKeyTree(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `boa:"noconfig" optional:"true"`
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"Other":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing", func(t *testing.T) {
		calls := 0
		err := (Cmd[Params]{
			Use:     "test",
			RunFunc: func(*Params, *cobra.Command, []string) {},
			ConfigFormat: ConfigFormat{Unmarshal: func(data []byte, target any) error {
				calls++
				return json.Unmarshal(data, target)
			}},
		}).RunArgsE([]string{"--config-file", path})
		if err == nil || !strings.Contains(err.Error(), "does not provide KeyTree") {
			t.Fatalf("expected missing-KeyTree error, got %v", err)
		}
		if calls != 0 {
			t.Fatalf("decoder called %d times", calls)
		}
	})

	t.Run("failing", func(t *testing.T) {
		sentinel := errors.New("probe failed")
		err := (Cmd[Params]{
			Use:     "test",
			RunFunc: func(*Params, *cobra.Command, []string) {},
			ConfigFormat: ConfigFormat{
				Unmarshal: json.Unmarshal,
				KeyTree: func([]byte) (map[string]any, error) {
					return nil, sentinel
				},
			},
		}).RunArgsE([]string{"--config-file", path})
		if err == nil || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "cannot inspect config") {
			t.Fatalf("expected failing-KeyTree error, got %v", err)
		}
	})

	t.Run("unrelated target remains compatible", func(t *testing.T) {
		type Plain struct {
			ConfigFile string `configfile:"true" optional:"true"`
			Other      string `optional:"true"`
		}
		var got string
		err := (Cmd[Plain]{
			Use:          "test",
			ConfigFormat: ConfigFormat{Unmarshal: json.Unmarshal},
			RunFunc: func(p *Plain, _ *cobra.Command, _ []string) {
				got = p.Other
			},
		}).RunArgsE([]string{"--config-file", path})
		if err != nil {
			t.Fatal(err)
		}
		if got != "value" {
			t.Fatalf("Other = %q, want value", got)
		}
	})

	t.Run("format-excluded field needs no key tree", func(t *testing.T) {
		type Excluded struct {
			ConfigFile string `configfile:"true" optional:"true"`
			Other      string `optional:"true"`
			Secret     string `boa:"noconfig" json:"-" optional:"true"`
		}
		var got string
		err := (Cmd[Excluded]{
			Use:          "test",
			ConfigFormat: ConfigFormat{Unmarshal: json.Unmarshal},
			RunFunc: func(p *Excluded, _ *cobra.Command, _ []string) {
				got = p.Other
			},
		}).RunArgsE([]string{"--config-file", path})
		if err != nil {
			t.Fatal(err)
		}
		if got != "value" {
			t.Fatalf("Other = %q, want value", got)
		}
	})
}

func TestNoConfig_ReloadRejectsForbiddenKey(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Name       string `optional:"true"`
		Secret     string `boa:"noconfig" optional:"true"`
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"Name":"first"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := (Cmd[Params]{
		Use: "test",
		RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
			if err := os.WriteFile(path, []byte(`{"Name":"second","Secret":"bad"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			fresh, err := Reload[Params](ctx)
			if err == nil || !strings.Contains(err.Error(), `config key "Secret"`) {
				t.Fatalf("expected reload rejection, got fresh=%+v err=%v", fresh, err)
			}
			if fresh != nil {
				t.Fatalf("fresh params should be nil on rejection: %+v", fresh)
			}
			if p.Name != "first" || p.Secret != "" {
				t.Fatalf("reload mutated live params: %+v", p)
			}
		},
	}).RunArgsE([]string{"--config-file", path})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoConfig_ManagedDumpOmitsNaiveDumpRetains(t *testing.T) {
	t.Setenv("BOA_NOCONFIG_DUMP_SECRET", "secret")
	type Params struct {
		Visible string `default:"visible"`
		Secret  string `boa:"noconfig" env:"BOA_NOCONFIG_DUMP_SECRET"`
	}

	err := (Cmd[Params]{
		Use: "test",
		RunFuncCtxE: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
			managed, err := ctx.DumpBytes(".json", nil)
			if err != nil {
				return err
			}
			var managedMap map[string]any
			if err := json.Unmarshal(managed, &managedMap); err != nil {
				return err
			}
			if _, ok := managedMap["Secret"]; ok {
				t.Fatalf("managed dump leaked noconfig field: %s", managed)
			}

			naive, err := DumpConfigBytes(p, ".json", nil)
			if err != nil {
				return err
			}
			var naiveMap map[string]any
			if err := json.Unmarshal(naive, &naiveMap); err != nil {
				return err
			}
			if naiveMap["Secret"] != "secret" {
				t.Fatalf("naive dump should retain raw struct field: %s", naive)
			}
			return nil
		},
	}).RunArgsE(nil)
	if err != nil {
		t.Fatal(err)
	}
}

func noConfigFile(t *testing.T, name, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNoConfig_AllNestedOccurrences(t *testing.T) {
	type Auth struct {
		Secret string `boa:"noconfig"`
	}
	type Params struct{ Auth Auth }
	for _, data := range []string{
		`{"Auth":{},"auth":{"Secret":"bad"}}`,
		`{"AUTH":{},"auth":{"Secret":"bad"}}`,
		`{"Auth":{"Secret":"bad"},"Auth":{}}`,
		`{"Auth":{"Secret":"bad"},"Auth":null}`,
		`{"Auth":{"Secret":null},"Auth":{}}`,
	} {
		t.Run(data, func(t *testing.T) {
			p := Params{Auth: Auth{Secret: "original"}}
			err := LoadConfigBytes([]byte(data), ".json", &p, nil)
			if err == nil || !strings.Contains(err.Error(), "noconfig") {
				t.Fatalf("expected rejection, got params=%+v err=%v", p, err)
			}
			if p.Auth.Secret != "original" {
				t.Fatalf("destination mutated before rejection: %+v", p)
			}
		})
	}
}

func TestNoConfig_JSONFallback(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `boa:"noconfig" json:"token" optional:"true"`
	}
	const data = `{"token":"bad"}`
	path := noConfigFile(t, "config.unregistered", data)
	for name, load := range map[string]func(*Params) error{
		"bytes": func(p *Params) error { return LoadConfigBytes([]byte(data), ".unregistered", p, nil) },
		"file":  func(p *Params) error { return LoadConfigFile(path, p, nil) },
		"command": func(p *Params) error {
			return (Cmd[Params]{Use: "test", Params: p, RunFunc: func(*Params, *cobra.Command, []string) {}}).
				RunArgsE([]string{"--config-file", path})
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := Params{Secret: "original"}
			if err := load(&p); err == nil || !strings.Contains(err.Error(), `config key "token"`) {
				t.Fatalf("expected fallback rejection, got %v", err)
			}
			if p.Secret != "original" {
				t.Fatalf("destination mutated before rejection: %+v", p)
			}
		})
	}
}

func TestNoConfig_MetadataOverridesTag(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `boa:"noconfig"`
	}
	path := noConfigFile(t, "config.json", `{"Secret":"allowed"}`)
	err := (Cmd[Params]{
		Use: "test",
		InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
			if !Param(ctx, &p.Secret).IsNoConfig() {
				t.Fatal("tag default should be visible before the init override")
			}
			Param(ctx, &p.Secret).SetNoConfig(false)
			return nil
		},
		RunFuncCtxE: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
			if p.Secret != "allowed" || Param(ctx, &p.Secret).IsNoConfig() {
				t.Fatalf("metadata override not applied: %+v", p)
			}
			data, err := ctx.DumpBytes(".json", nil)
			if err != nil || !strings.Contains(string(data), `"Secret": "allowed"`) {
				t.Fatalf("dump did not honor override: %s, %v", data, err)
			}
			fresh, err := Reload[Params](ctx)
			if err != nil || fresh.Secret != "allowed" {
				t.Fatalf("reload did not honor override: %+v, %v", fresh, err)
			}
			return nil
		},
	}).RunArgsE([]string{"--config-file", path})
	if err != nil {
		t.Fatal(err)
	}
}

func checkNoConfigGroupDump[T any](t *testing.T, wantKeys int) {
	t.Helper()
	err := (Cmd[T]{Use: "test", RunFuncCtxE: func(ctx *HookContext, _ *T, _ *cobra.Command, _ []string) error {
		data, err := ctx.DumpBytes(".json", nil)
		if err != nil {
			return err
		}
		var roundtrip T
		if err := LoadConfigBytes(data, ".json", &roundtrip, nil); err != nil {
			t.Fatalf("dump cannot load: %s: %v", data, err)
		}
		var keys map[string]any
		if err := json.Unmarshal(data, &keys); err != nil || len(keys) != wantKeys || (wantKeys > 0 && keys["Visible"] != "visible") {
			t.Fatalf("unexpected dump: %s, %v", data, err)
		}
		return nil
	}}).RunArgsE(nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoConfig_GroupDump(t *testing.T) {
	t.Setenv("BOA_NOCONFIG_GROUP", "secret")
	t.Setenv("AUTH_BOA_NOCONFIG_GROUP", "secret")
	type Auth struct {
		Secret string `env:"BOA_NOCONFIG_GROUP"`
	}
	t.Run("named", func(t *testing.T) {
		checkNoConfigGroupDump[struct {
			Visible string `default:"visible"`
			Auth    Auth   `boa:"noconfig"`
		}](t, 1)
	})
	t.Run("pointer", func(t *testing.T) {
		checkNoConfigGroupDump[struct {
			Visible string `default:"visible"`
			Auth    *Auth  `boa:"noconfig"`
		}](t, 1)
	})
	t.Run("embedded", func(t *testing.T) {
		checkNoConfigGroupDump[struct {
			Visible string `default:"visible"`
			Auth    `boa:"noconfig"`
		}](t, 1)
	})
	t.Run("named embedding", func(t *testing.T) {
		checkNoConfigGroupDump[struct {
			Visible string `default:"visible"`
			Auth    `boa:"noconfig" json:"auth"`
		}](t, 1)
	})
	t.Run("all excluded", func(t *testing.T) {
		checkNoConfigGroupDump[struct {
			Auth Auth `boa:"noconfig"`
		}](t, 0)
	})
}

func TestNoConfig_ProbeOnce(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `boa:"noconfig" optional:"true"`
		Count      int    `default:"0"`
	}
	calls := 0
	path := noConfigFile(t, "config.json", `{"Count":0}`)
	err := (Cmd[Params]{
		Use: "test",
		ConfigFormat: ConfigFormat{Unmarshal: json.Unmarshal, KeyTree: func(data []byte) (map[string]any, error) {
			calls++
			return jsonKeyTree(data)
		}},
		RunFuncCtxE: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
			if !Param(ctx, &p.Count).Parameter.(*paramMeta).setByConfig {
				t.Fatal("explicit zero lost its config presence")
			}
			return nil
		},
	}).RunArgsE([]string{"--config-file", path})
	if err != nil || calls != 1 {
		t.Fatalf("KeyTree calls=%d, want 1; error=%v", calls, err)
	}
}

func TestNoConfig_PresenceAfterOverlays(t *testing.T) {
	type Auth struct {
		Count  int    `optional:"true"`
		Secret string `boa:"noconfig" optional:"true"`
	}
	type Params struct {
		ConfigFile []string `configfile:"true" optional:"true"`
		Auth       *Auth
	}
	for _, tc := range []struct {
		name     string
		files    []string
		present  bool
		countSet bool
	}{
		{"absent", []string{`{}`}, false, false},
		{"empty group", []string{`{"Auth":{}}`}, true, false},
		{"case variants", []string{`{"Auth":{},"auth":{"Count":0}}`}, true, true},
		{"repeated group", []string{`{"Auth":{"Count":0},"Auth":{}}`}, true, true},
		{"cleared", []string{`{"Auth":{"Count":0}}`, `{"Auth":null}`}, false, false},
		{"repopulated", []string{`{"Auth":null}`, `{"Auth":{"Count":0}}`}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			for _, data := range tc.files {
				args = append(args, "--config-file", noConfigFile(t, "config.json", data))
			}
			err := (Cmd[Params]{Use: "test", RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
				if (p.Auth != nil) != tc.present {
					t.Fatalf("Auth = %+v, want present=%v", p.Auth, tc.present)
				}
				if p.Auth != nil && ctx.HasValue(&p.Auth.Count) != tc.countSet {
					t.Fatalf("Count presence = %v, want %v", ctx.HasValue(&p.Auth.Count), tc.countSet)
				}
			}}).RunArgsE(args)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNoConfig_MalformedJSONDoesNotDecode(t *testing.T) {
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Secret     string `boa:"noconfig" optional:"true"`
	}
	for _, data := range []string{"", "{", `{"Auth":`, `{"Auth": [}`, `{} {}`, `{} true`} {
		t.Run(data, func(t *testing.T) {
			calls := 0
			err := (Cmd[Params]{
				Use: "test",
				ConfigFormat: ConfigFormat{KeyTree: jsonKeyTree, Unmarshal: func([]byte, any) error {
					calls++
					return nil
				}},
				RunFunc: func(*Params, *cobra.Command, []string) {},
			}).RunArgsE([]string{"--config-file", noConfigFile(t, "config.json", data)})
			if err == nil || calls != 0 {
				t.Fatalf("malformed JSON reached decoder: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestNoConfig_NamedEmbeddingPresenceAndDump(t *testing.T) {
	type Embedded struct {
		Count  int    `optional:"true"`
		Secret string `boa:"noconfig" optional:"true"`
	}
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		*Embedded  `json:"auth"`
	}
	path := noConfigFile(t, "config.json", `{"auth":{"Count":0}}`)
	err := (Cmd[Params]{Use: "test", RunFuncCtxE: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
		if p.Embedded == nil || !ctx.HasValue(&p.Count) {
			t.Fatalf("explicit zero in embedded pointer lost: %+v", p)
		}
		data, err := ctx.DumpBytes(".json", nil)
		if err != nil {
			return err
		}
		var fresh Params
		if err := LoadConfigBytes(data, ".json", &fresh, nil); err != nil || fresh.Embedded == nil || fresh.Count != 0 {
			t.Fatalf("named embedding failed to round-trip: %s, %v", data, err)
		}
		return nil
	}}).RunArgsE([]string{"--config-file", path})
	if err != nil {
		t.Fatal(err)
	}
}

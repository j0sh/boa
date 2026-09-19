package boa

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func writeSecretTestFile(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	return path
}

func TestSecretFor_SourcesAndExactContents(t *testing.T) {
	secretBytes := []byte{'s', 'e', 'c', 'r', 'e', 't', '\n', 0xff, ' '}
	secretPath := writeSecretTestFile(t, "token", secretBytes)

	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Token      string `secret:"true" env:"BOA_SECRET_SOURCE_TOKEN"`
		TokenFile  string `secretfor:"Token" env:"BOA_SECRET_SOURCE_TOKEN_FILE" json:"token_path"`
	}

	tests := []struct {
		name string
		args func(t *testing.T) []string
		env  func(t *testing.T)
	}{
		{
			name: "flag",
			args: func(t *testing.T) []string { return []string{"--token-file", secretPath} },
		},
		{
			name: "environment",
			env:  func(t *testing.T) { t.Setenv("BOA_SECRET_SOURCE_TOKEN_FILE", secretPath) },
		},
		{
			name: "config with strict decoder",
			args: func(t *testing.T) []string {
				data, err := json.Marshal(map[string]string{"token_path": secretPath})
				if err != nil {
					t.Fatal(err)
				}
				configPath := writeSecretTestFile(t, "config.json", data)
				return []string{"--config-file", configPath}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_SECRET_SOURCE_TOKEN", "")
			t.Setenv("BOA_SECRET_SOURCE_TOKEN_FILE", "")
			if tc.env != nil {
				tc.env(t)
			}
			var got string
			var preValidateGot string
			cmd := Cmd[Params]{
				Use: "test",
				ConfigFormat: UniversalConfigFormat(func(data []byte, target any) error {
					return jsonv2.Unmarshal(data, target, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true))
				}),
				PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
					preValidateGot = p.Token
					return nil
				},
				RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Token },
			}
			var args []string
			if tc.args != nil {
				args = tc.args(t)
			}
			if err := cmd.RunArgsE(args); err != nil {
				t.Fatalf("RunArgsE: %v", err)
			}
			want := string(secretBytes)
			if got != want || preValidateGot != want {
				t.Fatalf("secret bytes were not preserved: run=%q prevalidate=%q want=%q", got, preValidateGot, want)
			}
		})
	}
}

func TestSecret_DirectEnvironmentAndHiddenFlag(t *testing.T) {
	type Params struct {
		Token     string `secret:"true" env:"BOA_SECRET_DIRECT_TOKEN" descr:"API token"`
		TokenFile string `secretfor:"Token"`
	}

	t.Setenv("BOA_SECRET_DIRECT_TOKEN", "direct")
	var got string
	cmd := Cmd[Params]{
		Use:     "test",
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Token },
	}
	if err := cmd.RunArgsE(nil); err != nil {
		t.Fatalf("direct secret environment value failed: %v", err)
	}
	if got != "direct" {
		t.Fatalf("Token=%q, want direct", got)
	}
	if err := cmd.RunArgsE([]string{"--token", "exposed"}); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected hidden secret flag to be rejected, got %v", err)
	}
	cobraCmd := cmd.ToCobra()
	var help strings.Builder
	cobraCmd.SetOut(&help)
	cobraCmd.SetArgs([]string{"--help"})
	if err := cobraCmd.Execute(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}
	usage := help.String()
	if !strings.Contains(usage, "\nEnvironment Variables:\n  BOA_SECRET_DIRECT_TOKEN  API token\n") || !strings.Contains(usage, "--token-file") {
		t.Errorf("secret help should list the environment source and file flag:\n%s", usage)
	}
	if strings.Contains(usage, "--token string") || strings.Contains(usage, "direct") {
		t.Errorf("direct secret flag or value appeared in help:\n%s", usage)
	}
}

func TestSecret_FalseIsOrdinaryField(t *testing.T) {
	type Params struct {
		Token string `secret:"false"`
	}
	var got string
	err := (Cmd[Params]{
		Use:     "test",
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Token },
	}).RunArgsE([]string{"--token", "visible"})
	if err != nil {
		t.Fatalf("secret:false should leave ordinary field behavior intact: %v", err)
	}
	if got != "visible" {
		t.Fatalf("Token=%q, want visible", got)
	}
}

func TestSecretFor_EmptyAndPointerNamedStrings(t *testing.T) {
	type Secret string
	type Params struct {
		Token     *Secret `secret:"true"`
		TokenFile *string `secretfor:"Token"`
	}

	path := writeSecretTestFile(t, "empty", nil)
	var got *Secret
	err := (Cmd[Params]{
		Use:     "test",
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Token },
	}).RunArgsE([]string{"--token-file", path})
	if err != nil {
		t.Fatalf("empty secret file should satisfy a required secret: %v", err)
	}
	if got == nil || *got != "" {
		t.Fatalf("Token=%v, want non-nil pointer to empty secret", got)
	}
}

func TestSecretFor_Conflicts(t *testing.T) {
	path := writeSecretTestFile(t, "token", []byte("from-file"))
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Token      string `secret:"true" env:"BOA_SECRET_CONFLICT_TOKEN"`
		TokenFile  string `secretfor:"Token" env:"BOA_SECRET_CONFLICT_TOKEN_FILE"`
	}

	tests := []struct {
		name string
		args func(t *testing.T) []string
		env  func(t *testing.T)
	}{
		{name: "direct env and file flag", args: func(t *testing.T) []string { return []string{"--token-file", path} }},
		{name: "direct env and file env", env: func(t *testing.T) { t.Setenv("BOA_SECRET_CONFLICT_TOKEN_FILE", path) }},
		{name: "direct env and file config", args: func(t *testing.T) []string {
			data, err := json.Marshal(map[string]string{"TokenFile": path})
			if err != nil {
				t.Fatal(err)
			}
			return []string{"--config-file", writeSecretTestFile(t, "config.json", data)}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOA_SECRET_CONFLICT_TOKEN", "direct")
			t.Setenv("BOA_SECRET_CONFLICT_TOKEN_FILE", "")
			if tc.env != nil {
				tc.env(t)
			}
			args := tc.args
			var runArgs []string
			if args != nil {
				runArgs = args(t)
			}
			err := (Cmd[Params]{Use: "test", RunFunc: func(*Params, *cobra.Command, []string) {}}).RunArgsE(runArgs)
			if err == nil || !IsUserInputError(err) || !strings.Contains(err.Error(), "token") || !strings.Contains(err.Error(), "token-file") {
				t.Fatalf("expected named UserInputError conflict, got %v", err)
			}
		})
	}

	t.Run("default and file", func(t *testing.T) {
		type DefaultParams struct {
			Token     string `secret:"true" default:"direct"`
			TokenFile string `secretfor:"Token"`
		}
		err := (Cmd[DefaultParams]{Use: "test", RunFunc: func(*DefaultParams, *cobra.Command, []string) {}}).
			RunArgsE([]string{"--token-file", path})
		if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
			t.Fatalf("expected default/file conflict, got %v", err)
		}
	})

	t.Run("programmatic value and file", func(t *testing.T) {
		params := &Params{Token: "direct"}
		err := (Cmd[Params]{
			Use:     "test",
			Params:  params,
			RunFunc: func(*Params, *cobra.Command, []string) {},
		}).RunArgsE([]string{"--token-file", path})
		if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
			t.Fatalf("expected programmatic/file conflict, got %v", err)
		}
	})
}

func TestSecretFor_FileErrorsAreUserInput(t *testing.T) {
	type Params struct {
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token"`
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		err := (Cmd[Params]{Use: "test", RunFunc: func(*Params, *cobra.Command, []string) {}}).
			RunArgsE([]string{"--token-file", path})
		if err == nil || !IsUserInputError(err) || !strings.Contains(err.Error(), path) {
			t.Fatalf("path %q: expected identifying UserInputError, got %v", path, err)
		}
	}
}

func TestSecretFor_PreValidateSecondPass(t *testing.T) {
	path := writeSecretTestFile(t, "token", []byte("hook-secret"))
	type Params struct {
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token"`
	}
	var got string
	err := (Cmd[Params]{
		Use: "test",
		PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
			p.TokenFile = path
			return nil
		},
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Token },
	}).RunArgsE(nil)
	if err != nil {
		t.Fatalf("PreValidate-supplied secret file failed: %v", err)
	}
	if got != "hook-secret" {
		t.Fatalf("Token=%q, want hook-secret", got)
	}

	err = (Cmd[Params]{
		Use: "test",
		PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
			p.Token = "hook-direct"
			return nil
		},
		RunFunc: func(*Params, *cobra.Command, []string) {},
	}).RunArgsE([]string{"--token-file", path})
	if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("expected post-hook conflict, got %v", err)
	}
}

func TestSecretFor_PreValidateReplacesPath(t *testing.T) {
	type Secret string
	type Params struct {
		Token       Secret  `secret:"true"`
		TokenFile   string  `secretfor:"Token"`
		Pointer     *Secret `secret:"true"`
		PointerFile *string `secretfor:"Pointer"`
	}
	first := writeSecretTestFile(t, "first", []byte("first"))
	for _, contents := range []string{"second", ""} {
		t.Run("replacement="+contents, func(t *testing.T) {
			second := writeSecretTestFile(t, "second", []byte(contents))
			params := &Params{TokenFile: first, PointerFile: &first}
			err := (Cmd[Params]{
				Use:    "test",
				Params: params,
				PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
					if p.Token != "first" || p.Pointer == nil || *p.Pointer != "first" {
						t.Fatalf("initial secrets not loaded: %+v", p)
					}
					p.TokenFile, p.PointerFile = second, &second
					return nil
				},
				RunFunc: func(p *Params, _ *cobra.Command, _ []string) {
					if string(p.Token) != contents {
						t.Errorf("Token=%q, want %q", p.Token, contents)
					}
					if p.Pointer == nil || string(*p.Pointer) != contents {
						t.Errorf("Pointer=%v, want pointer to %q", p.Pointer, contents)
					}
				},
			}).RunArgsE(nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSecretFor_IgnoredTarget(t *testing.T) {
	type Params struct {
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token"`
	}
	path := writeSecretTestFile(t, "token", []byte("from-file"))
	for _, defaultValue := range []string{"", "unused-default"} {
		t.Run("default="+defaultValue, func(t *testing.T) {
			err := (Cmd[Params]{
				Use: "test",
				InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
					field := Param(ctx, &p.Token)
					field.SetIgnored(true)
					if defaultValue != "" {
						field.SetDefault(defaultValue)
					}
					return nil
				},
				RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
					if p.Token != "" || ctx.HasValue(&p.Token) != (defaultValue != "") {
						t.Errorf("ignored target changed: Token=%q, HasValue=%v", p.Token, ctx.HasValue(&p.Token))
					}
				},
			}).RunArgsE([]string{"--token-file", path})
			if err != nil {
				t.Fatalf("ignored target should not conflict: %v", err)
			}
		})
	}
}

func TestSecret_ConfigPolicyDumpReloadAndWatch(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "token")
	if err := os.WriteFile(secretPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	configData, err := json.Marshal(map[string]string{"TokenFile": secretPath})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		Token      string `secret:"true"`
		TokenFile  string `secretfor:"Token"`
	}

	Cmd[Params]{
		Use: "test",
		RunFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) {
			if p.Token != "first" {
				t.Fatalf("initial Token=%q", p.Token)
			}
			if watched := ctx.WatchedConfigFiles(); !slices.Equal(watched, []string{configPath}) {
				t.Fatalf("watched files=%v, want only config file", watched)
			}
			dump, err := ctx.DumpBytes(".json", nil)
			if err != nil {
				t.Fatalf("DumpBytes: %v", err)
			}
			var dumped map[string]any
			if err := json.Unmarshal(dump, &dumped); err != nil {
				t.Fatalf("decode dump: %v", err)
			}
			if _, ok := dumped["Token"]; ok {
				t.Fatalf("secret leaked into dump: %s", dump)
			}
			if dumped["TokenFile"] != secretPath {
				t.Fatalf("dumped TokenFile=%v, want %q", dumped["TokenFile"], secretPath)
			}

			if err := os.WriteFile(secretPath, []byte("second"), 0o600); err != nil {
				t.Fatal(err)
			}
			fresh, err := Reload[Params](ctx)
			if err != nil {
				t.Fatalf("Reload: %v", err)
			}
			if fresh.Token != "second" {
				t.Fatalf("reloaded Token=%q, want second", fresh.Token)
			}
		},
	}.RunArgs([]string{"--config-file", configPath})

	directConfig := []byte(`{"Token":"leak"}`)
	if err := LoadConfigBytes(directConfig, ".json", &Params{}, nil); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("standalone config should reject direct secret, got %v", err)
	}
	err = (Cmd[Params]{Use: "test", RunFunc: func(*Params, *cobra.Command, []string) {}}).
		RunArgsE([]string{"--config-file", writeSecretTestFile(t, "direct.json", directConfig)})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("managed config should reject direct secret, got %v", err)
	}
}

func TestSecretTags_InvalidRelationships(t *testing.T) {
	assertSetupError := func(t *testing.T, cmd interface {
		ToCobraE() (*cobra.Command, error)
	}, contains string) {
		t.Helper()
		_, err := cmd.ToCobraE()
		if err == nil || !strings.Contains(err.Error(), contains) {
			t.Fatalf("expected setup error containing %q, got %v", contains, err)
		}
	}

	t.Run("invalid secret boolean", func(t *testing.T) {
		type P struct {
			Token string `secret:"yes"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "invalid secret value")
	})
	t.Run("non-string secret", func(t *testing.T) {
		type P struct {
			Token int `secret:"true"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "secret tag requires a string field")
	})
	t.Run("missing sibling", func(t *testing.T) {
		type P struct {
			TokenFile string `secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "same struct")
	})
	t.Run("target is not secret", func(t *testing.T) {
		type P struct {
			Token     string
			TokenFile string `secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, `must have secret:"true"`)
	})
	t.Run("both tags", func(t *testing.T) {
		type P struct {
			Token string `secret:"true" secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "both")
	})
	t.Run("self reference", func(t *testing.T) {
		type P struct {
			Token string `secret:"false" secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "cannot target itself")
	})
	t.Run("non-string companion", func(t *testing.T) {
		type P struct {
			Token     string `secret:"true"`
			TokenFile int    `secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "file tag requires a string field")
	})
	t.Run("duplicate companions", func(t *testing.T) {
		type P struct {
			Token string `secret:"true"`
			One   string `secretfor:"Token"`
			Two   string `secretfor:"Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "more than one")
	})
	t.Run("required true", func(t *testing.T) {
		type P struct {
			Token     string `secret:"true"`
			TokenFile string `secretfor:"Token" required:"true"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "implicitly optional")
	})
	t.Run("optional false", func(t *testing.T) {
		type P struct {
			Token     string `secret:"true"`
			TokenFile string `secretfor:"Token" optional:"false"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "implicitly optional")
	})
	t.Run("qualified target rejected", func(t *testing.T) {
		type Auth struct {
			Token string `secret:"true"`
		}
		type P struct {
			Auth      Auth
			TokenFile string `secretfor:"Auth.Token"`
		}
		assertSetupError(t, Cmd[P]{Use: "test"}, "same struct")
	})
}

func TestSecretFor_NestedSameStruct(t *testing.T) {
	type Auth struct {
		Token     string `secret:"true"`
		TokenFile string `secretfor:"Token"`
	}
	type Params struct{ Auth Auth }
	path := writeSecretTestFile(t, "nested", []byte("nested-secret"))
	var got string
	err := (Cmd[Params]{
		Use:     "test",
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) { got = p.Auth.Token },
	}).RunArgsE([]string{"--auth-token-file", path})
	if err != nil {
		t.Fatalf("nested secretfor failed: %v", err)
	}
	if got != "nested-secret" {
		t.Fatalf("Token=%q, want nested-secret", got)
	}

	t.Run("optional pointer group from config", func(t *testing.T) {
		type PointerParams struct {
			ConfigFile string `configfile:"true" optional:"true"`
			Auth       *Auth
		}
		data, err := json.Marshal(map[string]any{
			"Auth": map[string]string{"TokenFile": path},
		})
		if err != nil {
			t.Fatal(err)
		}
		configPath := writeSecretTestFile(t, "nested.json", data)
		var auth *Auth
		err = (Cmd[PointerParams]{
			Use:     "test",
			RunFunc: func(p *PointerParams, _ *cobra.Command, _ []string) { auth = p.Auth },
		}).RunArgsE([]string{"--config-file", configPath})
		if err != nil {
			t.Fatalf("nested config secretfor failed: %v", err)
		}
		if auth == nil || auth.Token != "nested-secret" {
			t.Fatalf("Auth=%+v, want populated nested secret", auth)
		}
	})
}

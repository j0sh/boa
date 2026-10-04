package boa

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestSecretURL_Sources(t *testing.T) {
	t.Run("URL", func(t *testing.T) {
		testSecretURLSources(t, func(value *url.URL) *url.URL { return value })
	})
	t.Run("Text", func(t *testing.T) {
		testSecretURLSources(t, func(value Text[*url.URL]) *url.URL { return value.Value })
	})
}

func testSecretURLSources[T any](t *testing.T, unwrap func(T) *url.URL) {
	t.Helper()
	type Params struct {
		ConfigFile   string `configfile:"true" optional:"true"`
		Endpoint     T      `secret:"true" env:"BOA_SECRET_URL" required:"true"`
		EndpointFile string `secretfor:"Endpoint" env:"BOA_SECRET_URL_FILE"`
	}
	const input = "https://user:private-token@example.com/api?key=private-key"
	path := writeSecretTestFile(t, "url", []byte(input))
	for _, source := range []string{"environment", "file flag", "file environment", "file config"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_SECRET_URL", "")
			t.Setenv("BOA_SECRET_URL_FILE", "")
			var args []string
			switch source {
			case "environment":
				t.Setenv("BOA_SECRET_URL", input)
			case "file flag":
				args = []string{"--endpoint-file", path}
			case "file environment":
				t.Setenv("BOA_SECRET_URL_FILE", path)
			case "file config":
				config := writeSecretTestFile(t, "config.json", fmt.Appendf(nil, `{"endpointfile":%q}`, path))
				args = []string{"--config-file", config}
			}
			validated, ran := false, false
			err := (Cmd[Params]{
				Use: "test", RejectUnknown: true,
				InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
					Param(ctx, &p.Endpoint).SetCustomValidator(func(value T) error {
						validated = true
						if u := unwrap(value); u == nil || u.String() != input {
							return errors.New("unexpected endpoint")
						}
						return nil
					})
					return nil
				},
				PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
					if u := unwrap(p.Endpoint); u == nil || u.String() != input {
						t.Fatal("endpoint unavailable before validation")
					}
					return nil
				},
				RunFuncCtxE: func(ctx *HookContext, p *Params, cmd *cobra.Command, _ []string) error {
					ran = true
					if !ctx.HasValue(&p.Endpoint) || cmd.Flags().Lookup("endpoint") != nil {
						t.Fatal("secret presence or hidden flag policy changed")
					}
					dump, err := ctx.DumpBytes(".json", nil)
					if err != nil || strings.Contains(string(dump), "Endpoint\"") || strings.Contains(string(dump), "private-token") {
						t.Fatalf("unsafe dump or error: %s, %v", dump, err)
					}
					if source != "environment" && !strings.Contains(string(dump), path) {
						t.Fatalf("companion path absent from dump: %s", dump)
					}
					return nil
				},
			}).RunArgsE(args)
			if err != nil || !validated || !ran {
				t.Fatalf("validated=%v ran=%v err=%v", validated, ran, err)
			}
		})
	}
	var p Params
	if err := (Cmd[Params]{Params: &p, RawArgs: []string{}}).Validate(); err == nil || !IsUserInputError(err) {
		t.Fatalf("missing required URL: %v", err)
	}
	if err := LoadConfigBytes([]byte(`{"Endpoint":"`+input+`"}`), ".json", &p, nil); err == nil {
		t.Fatal("direct secret config was accepted")
	}
}

type secretText struct {
	Value string
	Calls int
}

func (v *secretText) UnmarshalText(text []byte) error {
	v.Calls++
	if strings.HasPrefix(string(text), "reject") {
		return fmt.Errorf("rejected secret %s", text)
	}
	v.Value = strings.ToUpper(string(text))
	return nil
}

func (v secretText) MarshalText() ([]byte, error) { return []byte(v.Value), nil }

func TestSecretScalar_ParsersAndPointers(t *testing.T) {
	type NamedInt int
	type Params struct {
		Count       *NamedInt         `secret:"true"`
		CountFile   string            `secretfor:"Count"`
		Timeout     time.Duration     `secret:"true"`
		TimeoutFile string            `secretfor:"Timeout"`
		Custom      *secretText       `secret:"true"`
		CustomFile  string            `secretfor:"Custom"`
		Point       parsedConfigPoint `secret:"true"`
		PointFile   string            `secretfor:"Point"`
	}
	registerTypeCleanup(t, TypeDef[parsedConfigPoint]{
		Parse: func(text string) (parsedConfigPoint, error) {
			var p parsedConfigPoint
			_, err := fmt.Sscanf(text, "%d:%d", &p.X, &p.Y)
			return p, err
		},
		Format: func(p parsedConfigPoint) string { return fmt.Sprintf("%d:%d", p.X, p.Y) },
	})
	p := &Params{
		CountFile:   writeSecretTestFile(t, "count", []byte("0")),
		TimeoutFile: writeSecretTestFile(t, "timeout", []byte("2.5h")),
		CustomFile:  writeSecretTestFile(t, "custom", []byte("mixed\n")),
		PointFile:   writeSecretTestFile(t, "point", []byte("3:4")),
	}
	err := (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate()
	if err != nil || p.Count == nil || *p.Count != 0 || p.Timeout != 150*time.Minute ||
		p.Custom == nil || p.Custom.Value != "MIXED\n" || p.Custom.Calls != 1 || p.Point != (parsedConfigPoint{3, 4}) {
		t.Fatalf("scalar parsing failed: %+v, %v", p, err)
	}
}

func TestSecretScalar_RegisteredCollection(t *testing.T) {
	type Scalar []string
	registerTypeCleanup(t, TypeDef[Scalar]{
		Parse:  func(text string) (Scalar, error) { return Scalar{text}, nil },
		Format: func(value Scalar) string { return strings.Join(value, "") },
	})
	type Params struct {
		Value Scalar `secret:"true"`
		File  string `secretfor:"Value"`
	}
	p := &Params{File: writeSecretTestFile(t, "scalar", []byte("a,b"))}
	if err := (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate(); err != nil || len(p.Value) != 1 || p.Value[0] != "a,b" {
		t.Fatalf("registered scalar collection: %+v, %v", p, err)
	}
	type Bad struct {
		Value map[string]string `secret:"true"`
	}
	if _, err := (Cmd[Bad]{}).ToCobraE(); err == nil || !strings.Contains(err.Error(), "text scalar") {
		t.Fatalf("unregistered map secret accepted: %v", err)
	}
}

func TestSecretURL_FileHookLifecycle(t *testing.T) {
	type Params struct {
		Endpoint     *url.URL `secret:"true"`
		EndpointFile string   `secretfor:"Endpoint"`
	}
	for _, action := range []string{"unchanged", "new path", "hook path", "replace value", "mutate value"} {
		t.Run(action, func(t *testing.T) {
			first := writeSecretTestFile(t, "first", []byte("https://example.com/first"))
			second := writeSecretTestFile(t, "second", []byte("https://example.com/second"))
			p := &Params{EndpointFile: first}
			if action == "hook path" {
				p.EndpointFile = ""
			}
			err := (Cmd[Params]{Params: p, RawArgs: []string{}, PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
				switch action {
				case "new path", "hook path":
					p.EndpointFile = second
				case "replace value":
					p.Endpoint, _ = url.Parse("https://example.com/direct")
				case "mutate value":
					p.Endpoint.Path = "/direct"
				case "unchanged":
					// The second pass must not reread this file.
					return os.WriteFile(first, []byte("invalid\n"), 0o600)
				}
				return nil
			}}).Validate()
			if strings.Contains(action, "value") {
				if err == nil || !IsUserInputError(err) || !strings.Contains(err.Error(), "cannot both be set") {
					t.Fatalf("missing conflict: %v", err)
				}
			} else if err != nil || p.Endpoint == nil {
				t.Fatalf("resolution failed: %v", err)
			} else if action != "unchanged" && p.Endpoint.Path != "/second" {
				t.Fatalf("new path not applied: %v", p.Endpoint)
			}
		})
	}
}

func TestSecretURL_EmptyAndWhitespace(t *testing.T) {
	type Params struct {
		Endpoint *url.URL `secret:"true" required:"true"`
		File     string   `secretfor:"Endpoint"`
	}
	p := &Params{File: writeSecretTestFile(t, "empty", nil)}
	if err := (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate(); err != nil || p.Endpoint != nil {
		t.Fatalf("explicit nil parsed value must satisfy presence: %+v, %v", p, err)
	}
	p.File = writeSecretTestFile(t, "newline", []byte("https://example.com\n"))
	if err := (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate(); err == nil || !IsUserInputError(err) || p.Endpoint != nil {
		t.Fatalf("newline must reach URL parser unchanged: %+v, %v", p, err)
	}
}

func TestSecretScalar_ParseErrorsAreSafe(t *testing.T) {
	const input = "reject-private-sentinel\n"
	type Params struct {
		Value *secretText `secret:"true" env:"BOA_BAD_SECRET"`
		File  string      `secretfor:"Value"`
	}
	for _, source := range []string{"environment", "file", "default", "URL"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_BAD_SECRET", "")
			var err error
			p := &Params{}
			switch source {
			case "environment":
				t.Setenv("BOA_BAD_SECRET", input)
				err = (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate()
			case "file":
				p.File = writeSecretTestFile(t, "bad", []byte(input))
				err = (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate()
			case "default":
				type Defaults struct {
					Value secretText `secret:"true" default:"reject-private-sentinel"`
				}
				_, err = (Cmd[Defaults]{}).ToCobraE()
			case "URL":
				type URLs struct {
					Value *url.URL `secret:"true"`
					File  string   `secretfor:"Value"`
				}
				params := &URLs{File: writeSecretTestFile(t, "bad-url", []byte("https://user:reject-private-sentinel@example.com/%zz"))}
				err = (Cmd[URLs]{Params: params, RawArgs: []string{}}).Validate()
				if params.Value != nil {
					t.Fatal("failed URL parse modified target")
				}
			}
			if err == nil || p.Value != nil || source != "default" && !IsUserInputError(err) {
				t.Fatalf("expected safe failure without partial assignment: %v", err)
			}
			for nested := err; nested != nil; nested = errors.Unwrap(nested) {
				if strings.Contains(nested.Error(), "private-sentinel") {
					t.Fatalf("secret exposed by error: %v", nested)
				}
			}
			if !strings.Contains(strings.ToLower(err.Error()), "value") {
				t.Fatalf("error does not identify target: %v", err)
			}
			if source == "environment" && !strings.Contains(err.Error(), "BOA_BAD_SECRET") {
				t.Fatalf("error does not identify environment: %v", err)
			}
		})
	}
}

func TestSecretURL_ConflictsAndReload(t *testing.T) {
	type Params struct {
		Endpoint *url.URL `secret:"true" env:"BOA_CONFLICT_URL"`
		File     string   `secretfor:"Endpoint"`
	}
	path := writeSecretTestFile(t, "url", []byte("https://example.com/first"))
	for _, source := range []string{"environment", "programmatic", "default"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BOA_CONFLICT_URL", "")
			p := &Params{File: path}
			var err error
			switch source {
			case "environment":
				t.Setenv("BOA_CONFLICT_URL", "https://example.com/direct")
			case "programmatic":
				p.Endpoint, _ = url.Parse("https://example.com/direct")
			case "default":
				type Defaults struct {
					Endpoint *url.URL `secret:"true" default:"https://example.com/direct"`
					File     string   `secretfor:"Endpoint"`
				}
				err = (Cmd[Defaults]{Params: &Defaults{File: path}, RawArgs: []string{}}).Validate()
			}
			if source != "default" {
				err = (Cmd[Params]{Params: p, RawArgs: []string{}}).Validate()
			}
			if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
				t.Fatalf("missing conflict: %v", err)
			}
		})
	}
	t.Setenv("BOA_CONFLICT_URL", "")
	err := (Cmd[Params]{Use: "test", RunFuncCtxE: func(ctx *HookContext, p *Params, _ *cobra.Command, _ []string) error {
		if err := os.WriteFile(path, []byte("https://example.com/second"), 0o600); err != nil {
			return err
		}
		fresh, err := Reload[Params](ctx)
		if err != nil || fresh.Endpoint == nil || fresh.Endpoint.Path != "/second" || p.Endpoint.Path != "/first" {
			t.Fatalf("typed secret reload failed: %v", err)
		}
		if len(ctx.WatchedConfigFiles()) != 0 {
			t.Fatal("secret file added to config watches")
		}
		return nil
	}}).RunArgsE([]string{"--file", path})
	if err != nil {
		t.Fatal(err)
	}
}

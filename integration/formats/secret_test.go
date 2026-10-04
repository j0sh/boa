package formats_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestTypedSecretConfigPaths(t *testing.T) {
	t.Run("URL", func(t *testing.T) {
		testTypedSecretConfigPaths(t, func(v *url.URL) *url.URL { return v })
	})
	t.Run("Text", func(t *testing.T) {
		testTypedSecretConfigPaths(t, func(v boa.Text[*url.URL]) *url.URL { return v.Value })
	})
}

func testTypedSecretConfigPaths[T any](t *testing.T, unwrap func(T) *url.URL) {
	t.Helper()
	type Config struct {
		ConfigFile   string `configfile:"true" optional:"true"`
		Endpoint     T      `secret:"true" required:"true"`
		EndpointFile string `secretfor:"Endpoint"`
	}
	const endpoint = "https://user:private-sentinel@example.com/api"
	secretPath := writeConfig(t, ".secret", []byte(endpoint))
	for _, format := range []struct {
		name, ext, assignment string
		decode                func([]byte, any) error
		encode                func(any) ([]byte, error)
	}{
		{"burntsushi", ".toml", "endpointfile = %q\n", burnttoml.Unmarshal, burnttoml.Marshal},
		{"pelletier", ".toml", "endpointfile = %q\n", toml.Unmarshal, toml.Marshal},
		{"yaml", ".yaml", "endpointfile: %q\n", yaml.Unmarshal, yaml.Marshal},
	} {
		t.Run(format.name, func(t *testing.T) {
			boa.RegisterConfigFormat(format.ext, format.decode)
			path := writeConfig(t, format.ext, fmt.Appendf(nil, format.assignment, secretPath))
			p := &Config{ConfigFile: path}
			ran := false
			err := (boa.Cmd[Config]{Use: "test", Params: p, RejectUnknown: true,
				RunFuncCtxE: func(ctx *boa.HookContext, p *Config, _ *cobra.Command, _ []string) error {
					ran = true
					if u := unwrap(p.Endpoint); u == nil || u.String() != endpoint || !ctx.HasValue(&p.Endpoint) {
						t.Fatal("untagged companion did not populate typed secret")
					}
					dump, err := ctx.DumpBytes(format.ext, format.encode)
					if err != nil {
						return err
					}
					if strings.Contains(string(dump), "private-sentinel") || !strings.Contains(string(dump), secretPath) {
						t.Fatalf("unsafe or incomplete dump: %s", dump)
					}
					fresh := &Config{ConfigFile: writeConfig(t, format.ext, dump)}
					if err := (boa.Cmd[Config]{Params: fresh, RawArgs: []string{}, RejectUnknown: true}).Validate(); err != nil {
						return err
					}
					if u := unwrap(fresh.Endpoint); u == nil || u.String() != endpoint {
						t.Fatal("dump did not round-trip")
					}
					return nil
				},
			}).RunArgsE(nil)
			if err != nil || !ran {
				t.Fatalf("ran=%v err=%v", ran, err)
			}
			forbidden := fmt.Appendf(nil, strings.Replace(format.assignment, "endpointfile", "endpoint", 1), endpoint)
			if err := boa.LoadConfigBytes(forbidden, format.ext, &Config{}, nil); err == nil || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatalf("direct typed secret must be safely rejected: %v", err)
			}
		})
	}
}

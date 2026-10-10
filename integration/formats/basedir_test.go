package formats_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestBaseDirFormatPathsAndForbiddenKeys(t *testing.T) {
	type Params struct {
		Dir    string `basedir:"required,autocreate" json:"base" yaml:"base" toml:"base"`
		Config string `configfile:"true" optional:"true"`
		Input  string `file:"true" json:"input" yaml:"input" toml:"input"`
		Key    string `file:"true" basepath:"source" json:"key" yaml:"key" toml:"key"`
	}
	type Forbidden struct {
		Dir string `basedir:"true" boa:"noconfig" json:"base" yaml:"base" toml:"base"`
	}
	for _, tc := range []struct {
		name, ext, data string
		decode          func([]byte, any) error
	}{
		{"json", ".json", `{"base":"other","input":"input.txt","key":"key.txt"}`, boa.UnmarshalJSON},
		{"yaml", ".yaml", "base: other\ninput: input.txt\nkey: key.txt\n", yaml.Unmarshal},
		{"burntsushi", ".toml", "base = \"other\"\ninput = \"input.txt\"\nkey = \"key.txt\"\n", burnttoml.Unmarshal},
		{"pelletier", ".toml", "base = \"other\"\ninput = \"input.txt\"\nkey = \"key.txt\"\n", toml.Unmarshal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			final := filepath.Join(dir, "other")
			if err := os.Mkdir(final, 0o700); err != nil {
				t.Fatal(err)
			}
			input, key := filepath.Join(final, "input.txt"), filepath.Join(dir, "key.txt")
			for _, path := range []string{input, key} {
				if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(dir, "config"+tc.ext)
			if err := os.WriteFile(config, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var p Params
			cmd := boa.Cmd[Params]{Use: "test", Params: &p, RawArgs: []string{"--config", filepath.Base(config)}, RejectUnknown: true, ConfigFormat: boa.UniversalConfigFormat(tc.decode)}
			if err := cmd.Validate(); err != nil {
				t.Fatal(err)
			}
			if p.Dir != final || p.Input != input || p.Config != config || p.Key != key {
				t.Fatalf("unresolved paths: %+v", p)
			}
			p = Params{}
			if err := boa.LoadConfigFile(config, &p, tc.decode); err != nil {
				t.Fatal(err)
			}
			if p.Dir != "other" || p.Input != "input.txt" || p.Key != "key.txt" {
				t.Fatalf("standalone loader resolved paths: %+v", p)
			}
			if err := os.Remove(input); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(final); err != nil {
				t.Fatal(err)
			}
			p = Params{}
			if err := boa.LoadConfigBytes([]byte(tc.data), tc.ext, &p, tc.decode); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(final); !os.IsNotExist(err) {
				t.Fatalf("standalone loader created base: %v", err)
			}
			cmd.InitFuncCtx = func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
				boa.Param(ctx, &p.Dir).SetNoConfig(true)
				return nil
			}
			p = Params{}
			if err := cmd.Validate(); !boa.IsUserInputError(err) || !strings.Contains(err.Error(), "forbidden") {
				t.Fatalf("managed load must reject explicit noconfig: %v", err)
			}
			var forbidden Forbidden
			if err := boa.LoadConfigFile(config, &forbidden, tc.decode); err == nil || !strings.Contains(err.Error(), "forbidden") {
				t.Fatalf("standalone file must reject explicit noconfig: %v", err)
			}
			if err := boa.LoadConfigBytes([]byte(tc.data), tc.ext, &forbidden, tc.decode); err == nil || !strings.Contains(err.Error(), "forbidden") {
				t.Fatalf("standalone bytes must reject explicit noconfig: %v", err)
			}
		})
	}
}

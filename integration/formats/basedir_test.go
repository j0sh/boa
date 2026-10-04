package formats_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func TestBaseDirFormatPathsAndForbiddenKeys(t *testing.T) {
	type Params struct {
		Dir    string `basedir:"true" json:"base" yaml:"base" toml:"base"`
		Config string `configfile:"true" optional:"true"`
		Input  string `file:"true" json:"input" yaml:"input" toml:"input"`
	}
	for _, tc := range []struct {
		name, ext, data, forbidden string
		decode                     func([]byte, any) error
	}{
		{"json", ".json", `{"input":"input.txt"}`, `{"base":"other","input":"input.txt"}`, boa.UnmarshalJSON},
		{"yaml", ".yaml", "input: input.txt\n", "base: other\ninput: input.txt\n", yaml.Unmarshal},
		{"burntsushi", ".toml", "input = \"input.txt\"\n", "base = \"other\"\ninput = \"input.txt\"\n", burnttoml.Unmarshal},
		{"pelletier", ".toml", "input = \"input.txt\"\n", "base = \"other\"\ninput = \"input.txt\"\n", toml.Unmarshal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			want := filepath.Join(dir, "input.txt")
			if err := os.WriteFile(want, []byte("data"), 0o600); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(dir, "config"+tc.ext)
			if err := os.WriteFile(config, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var p Params
			cmd := boa.Cmd[Params]{Use: "test", Params: &p, RawArgs: []string{"--dir", dir, "--config", filepath.Base(config)}, RejectUnknown: true, ConfigFormat: boa.UniversalConfigFormat(tc.decode)}
			if err := cmd.Validate(); err != nil {
				t.Fatal(err)
			}
			if p.Input != want || p.Config != config {
				t.Fatalf("unresolved paths: %+v", p)
			}
			if err := os.WriteFile(config, []byte(tc.forbidden), 0o600); err != nil {
				t.Fatal(err)
			}
			p = Params{}
			if err := cmd.Validate(); !boa.IsUserInputError(err) || !strings.Contains(err.Error(), "forbidden") {
				t.Fatalf("managed load must reject base key: %v", err)
			}
			if err := boa.LoadConfigFile(config, &p, tc.decode); err == nil {
				t.Fatal("standalone load accepted base key")
			}
			if err := boa.LoadConfigBytes([]byte(tc.forbidden), tc.ext, &p, tc.decode); err == nil {
				t.Fatal("standalone bytes accepted base key")
			}
		})
	}
}

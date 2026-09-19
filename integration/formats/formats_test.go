package formats_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestPortableTextAndNativeDates(t *testing.T) {
	type Config struct {
		At      time.Time               `json:"at" yaml:"at" toml:"at"`
		Timeout boa.Text[time.Duration] `json:"timeout" yaml:"timeout" toml:"timeout"`
	}
	for _, tc := range []struct {
		name, data string
		decode     func([]byte, any) error
	}{
		{"json", `{"at":"2026-09-17","timeout":"2.5h"}`, boa.UnmarshalJSON},
		{"yaml", "at: 2026-09-17\ntimeout: 2.5h\n", yaml.Unmarshal},
		{"burntsushi", "at = 2026-09-17\ntimeout = \"2.5h\"\n", burnttoml.Unmarshal},
		{"pelletier", "at = 2026-09-17\ntimeout = \"2.5h\"\n", toml.Unmarshal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p Config
			if err := boa.LoadConfigBytes([]byte(tc.data), ".custom", &p, tc.decode); err != nil {
				t.Fatal(err)
			}
			if p.At.Format(time.DateOnly) != "2026-09-17" || p.Timeout.Value != 150*time.Minute {
				t.Fatalf("p=%+v", p)
			}
		})
	}
}

var rejected = errors.New("limit must not exceed 10")

type yamlConfig struct {
	Limit   int           `yaml:"limit"`
	Timeout time.Duration `yaml:"timeout"`
	Calls   int           `yaml:"-"`
}

func (c *yamlConfig) UnmarshalYAML(node *yaml.Node) error {
	c.Calls++
	type plain yamlConfig
	if err := node.Decode((*plain)(c)); err != nil {
		return err
	}
	if c.Limit > 10 {
		return rejected
	}
	return nil
}

func TestYAMLValidationCannotBeBypassed(t *testing.T) {
	for _, timeout := range []string{"2.5h", "9000000000000"} {
		var c yamlConfig
		err := boa.LoadConfigBytes([]byte("limit: 11\ntimeout: "+timeout), ".yaml", &c, yaml.Unmarshal)
		// YAML itself rejects numeric duration values; neither error may be hidden.
		if err == nil || c.Calls != 1 {
			t.Fatalf("calls=%d error=%v", c.Calls, err)
		}
		if timeout == "2.5h" && !errors.Is(err, rejected) {
			t.Fatalf("lost validation error: %v", err)
		}
	}
}

type tomlConfig struct {
	Calls   int
	Timeout time.Duration
}

func (c *tomlConfig) UnmarshalTOML(any) error { c.Calls++; return rejected }
func TestTOMLValidationCannotBeBypassed(t *testing.T) {
	var c tomlConfig
	err := boa.LoadConfigBytes([]byte("limit = 11\ntimeout = \"2.5h\""), ".toml", &c, burnttoml.Unmarshal)
	if err == nil || !strings.Contains(err.Error(), rejected.Error()) || c.Calls != 1 {
		t.Fatalf("calls=%d error=%v", c.Calls, err)
	}
}

func TestNativeTOMLDateWithDuration(t *testing.T) {
	var c struct {
		At      time.Time
		Timeout time.Duration
	}
	err := boa.LoadConfigBytes([]byte("At = 2026-09-17\nTimeout = \"1h\""), ".toml", &c, burnttoml.Unmarshal)
	if err != nil || c.At.Format(time.DateOnly) != "2026-09-17" || c.Timeout != time.Hour {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestNoConfigRegisteredFormats(t *testing.T) {
	type Config struct {
		Visible string `toml:"visible" yaml:"visible"`
		Secret  string `boa:"noconfig" toml:"token" yaml:"token"`
	}
	for _, tc := range []struct {
		name, ext, allowed, forbidden string
		decode                        func([]byte, any) error
	}{
		{"burntsushi", ".toml", "visible = \"changed\"\n", "token = \"bad\"\n", burnttoml.Unmarshal},
		{"pelletier", ".toml", "visible = \"changed\"\n", "token = \"bad\"\n", toml.Unmarshal},
		{"yaml", ".yaml", "visible: changed\n", "token: bad\n", yaml.Unmarshal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boa.RegisterConfigFormat(tc.ext, tc.decode)
			p := Config{Visible: "original", Secret: "original"}
			err := boa.LoadConfigBytes([]byte(tc.allowed+tc.forbidden), tc.ext, &p, nil)
			if err == nil || !strings.Contains(err.Error(), `config key "token"`) {
				t.Fatalf("expected noconfig rejection, got %v", err)
			}
			if p.Visible != "original" || p.Secret != "original" {
				t.Fatalf("target mutated before rejection: %+v", p)
			}
			if err := boa.LoadConfigBytes([]byte(tc.allowed), tc.ext, &p, nil); err != nil || p.Visible != "changed" || p.Secret != "original" {
				t.Fatalf("allowed keys failed to load: %+v, %v", p, err)
			}
		})
	}
}

func TestYAMLEmbeddedGroupPresence(t *testing.T) {
	type Embedded struct {
		Count  int    `optional:"true"`
		Secret string `boa:"noconfig" optional:"true"`
	}
	type Params struct {
		ConfigFile string `configfile:"true" optional:"true"`
		*Embedded
	}
	boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("embedded:\n  count: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := (boa.Cmd[Params]{Use: "test", RunFuncCtxE: func(ctx *boa.HookContext, p *Params, _ *cobra.Command, _ []string) error {
		if p.Embedded == nil || !ctx.HasValue(&p.Count) {
			t.Fatalf("explicit zero in YAML embedding lost: %+v", p)
		}
		data, err := ctx.DumpBytes(".yaml", yaml.Marshal)
		if err != nil {
			return err
		}
		var fresh Params
		if err := boa.LoadConfigBytes(data, ".yaml", &fresh, nil); err != nil || fresh.Embedded == nil || fresh.Count != 0 {
			t.Fatalf("YAML embedding failed to round-trip: %s, %v", data, err)
		}
		return nil
	}}).RunArgsE([]string{"--config-file", path})
	if err != nil {
		t.Fatal(err)
	}
	var p Params
	if err := boa.LoadConfigBytes([]byte("embedded:\n  secret: bad\n"), ".yaml", &p, nil); err == nil {
		t.Fatal("nested YAML secret was not rejected")
	}
	var inline struct {
		*Embedded `yaml:",inline"`
	}
	if err := boa.LoadConfigBytes([]byte("secret: bad\n"), ".yaml", &inline, nil); err == nil {
		t.Fatal("inline YAML secret was not rejected")
	}
}

package formats_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	burnttoml "github.com/BurntSushi/toml"
	"github.com/GiGurra/boa/pkg/boa"
	"github.com/pelletier/go-toml/v2"
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

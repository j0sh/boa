package boa

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type strictCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Webhook  *struct {
		Authorize bool `json:"authorize"`
	} `json:"webhook"`
}
type strictHidden struct {
	Label string `json:"label" optional:"true"`
}
type StrictPromoted struct {
	Name string `optional:"true"`
}
type StrictConflict StrictPromoted
type strictConfig struct {
	Source      string                      `configfile:"true" boa:"configonly" json:"-"`
	Credentials []strictCredential          `boa:"configonly" optional:"true" json:"credentials"`
	ByName      map[string]strictCredential `boa:"configonly" optional:"true" json:"by_name"`
	Metadata    map[string]any              `boa:"ignore" json:"metadata"`
	Timeout     time.Duration               `optional:"true" json:"timeout"`
	*StrictPromoted
	strictHidden
	Named  *StrictPromoted `json:"named"`
	Lower  int             `json:"fold" optional:"true"`
	Upper  string          `json:"FOLD" optional:"true"`
	Inline struct {
		Value string `json:"value"`
	} `boa:"ignore" json:",inline"`
	Extra     map[string]any          `boa:"ignore" json:",inline"`
	CaseExact string                  `json:"case_exact,case:strict" optional:"true"`
	Array     [2]strictCredential     `boa:"ignore" json:"array"`
	Value     validatedJSONConfig     `boa:"ignore" json:"value"`
	Custom    strictCustomCredentials `boa:"configonly" optional:"true" json:"custom"`
	Ambiguous struct {
		StrictPromoted
		StrictConflict
		Fallback string `json:"NAME"`
	} `boa:"ignore" json:"ambiguous"`
}
type strictCustomCredentials []strictCredential

func (p *strictCustomCredentials) UnmarshalJSON(data []byte) error {
	if string(data) == `"custom"` {
		*p = strictCustomCredentials{{Username: "custom"}}
		return nil
	}
	type plain strictCustomCredentials
	return UnmarshalJSON(data, (*plain)(p))
}

func TestRejectUnknown_ConfigFields(t *testing.T) {
	for _, tc := range []struct{ name, data, want string }{
		{"known", `{"CREDENTIALS":[{"username":"a","password":"secret","webhook":{"authorize":true}},{"username":"b"}],"by_name":{"tenant":{"username":"c"}},"metadata":{"arbitrary":{"key":true}},"timeout":"2.5h"}`, ""},
		{"embedding", `{"name":"flat","label":"exported","named":{"name":"nested"},"array":[{"username":"a"},{"username":"b"}]}`, ""},
		{"ignored inline tag", `{"Inline":{"value":"nested"},"Extra":{"dynamic":true},"case_exact":"known"}`, ""},
		{"folded", `{"FoLd":7}`, ""},
		{"exact", `{"FOLD":"exact"}`, ""},
		{"ambiguous exact with folded fallback", `{"ambiguous":{"Name":"fallback"}}`, ""},
		{"custom methods", `{"value":{"limit":10,"timeout":"2.5h"},"custom":"custom"}`, ""},
		{"custom validation", `{"value":{"limit":11,"timeout":"2.5h"}}`, "limit exceeds 10"},
		{"root", `{"credentials":[],"typo":"sensitive-value"}`, "typo"},
		{"excluded", `{"Source":"sensitive-value"}`, "Source"},
		{"entry", `{"credentials":[{"username":"a","pasword":"sensitive-value"}]}`, "credentials[0].pasword"},
		{"later entry", `{"credentials":[{"username":"a"},{"pasword":"sensitive-value"}]}`, "credentials[1].pasword"},
		{"pointer block", `{"credentials":[{"webhook":{"authorize":true,"typo":"sensitive-value"}}]}`, "credentials[0].webhook.typo"},
		{"map value", `{"by_name":{"tenant":{"pasword":"sensitive-value"}}}`, "by_name.tenant.pasword"},
		{"repeated group", `{"by_name":{"tenant":{"pasword":"sensitive-value"}},"by_name":{"tenant":{"username":"a"}}}`, "by_name.tenant.pasword"},
		{"repeated collection", `{"credentials":[{}, {"pasword":"sensitive-value"}],"credentials":[]}`, "credentials[1].pasword"},
		{"cleared collection", `{"credentials":[{"pasword":"sensitive-value"}],"credentials":null}`, "credentials[0].pasword"},
		{"shape changed", `{"credentials":[{"pasword":"sensitive-value"}],"credentials":{}}`, "shape changed"},
		{"named", `{"named":{"typo":true}}`, "named.typo"},
		{"array", `{"array":[{}, {"typo":true}]}`, "array[1].typo"},
		{"inline is not a catchall", `{"dynamic":true}`, "dynamic"},
		{"case sensitive", `{"CASE_EXACT":"hidden"}`, "CASE_EXACT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfigFile(t, tc.data)
			p := strictConfig{Source: path, Timeout: time.Hour}
			native := p
			nativeErr := UnmarshalJSON([]byte(tc.data), &native)
			err := (Cmd[strictConfig]{Params: &p, RawArgs: []string{}, RejectUnknown: true}).Validate()
			if tc.want == "" {
				if err != nil || nativeErr != nil || !reflect.DeepEqual(p, native) {
					t.Fatalf("strict=%+v native=%+v errors=%v/%v", p, native, err, nativeErr)
				}
			} else {
				if !IsUserInputError(err) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "sensitive-value") {
					t.Fatalf("want safe error identifying %q and file: %v", tc.want, err)
				}
				if p.Credentials != nil || p.Timeout != time.Hour || p.Value != native.Value {
					t.Fatalf("rejection changed config values or custom decoding: %+v", p)
				}
			}
		})
	}
}

func TestRejectUnknown_Probes(t *testing.T) { testConfigProbes(t, true) }

// The same probe contract protects strict decoding and noconfig independently.
func testConfigProbes(t *testing.T, strict bool) {
	t.Helper()
	sentinel := errors.New("probe failed")
	for _, tc := range []struct {
		name, data, want string
		probe            func([]byte) (map[string]any, error)
		probes, decodes  int
	}{
		{"known", `{}`, "", jsonKeyTree, 1, 1},
		{"unknown", `{"typo":true}`, "unknown config field", jsonKeyTree, 1, 0},
		{"missing", `{}`, "does not provide KeyTree", nil, 0, 0},
		{"nil", `{}`, "KeyTree returned nil", func([]byte) (map[string]any, error) { return nil, nil }, 1, 0},
		{"error", `{}`, "probe failed", func([]byte) (map[string]any, error) { return nil, sentinel }, 1, 0},
		{"opaque collection", `{"credentials":true}`, "retain collection elements", jsonKeyTree, 1, 0},
		{"non-string key", `{}`, "object keys must be strings", func([]byte) (map[string]any, error) {
			return map[string]any{"credentials": []any{map[any]any{1: true}}}, nil
		}, 1, 0},
	} {
		if !strict && (tc.name == "unknown" || tc.name == "opaque collection" || tc.name == "non-string key") {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var target any = &strictConfig{}
			if !strict {
				target = &struct {
					Secret string `boa:"noconfig"`
				}{}
			}
			probes, decodes := 0, 0
			format := ConfigFormat{Unmarshal: func([]byte, any) error { decodes++; return nil }}
			if tc.probe != nil {
				format.KeyTree = func(data []byte) (map[string]any, error) { probes++; return tc.probe(data) }
			}
			_, err := loadConfigBytesInto([]byte(tc.data), ".json", target, format, nil, strict)
			if probes != tc.probes || decodes != tc.decodes || (err != nil) != (tc.want != "") || err != nil && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("probes=%d decodes=%d err=%v", probes, decodes, err)
			}
			if tc.name == "error" && !errors.Is(err, sentinel) {
				t.Fatalf("lost probe error: %v", err)
			}
		})
	}
}

func TestRejectUnknown_NestedAndOverlayFiles(t *testing.T) {
	type Auth struct {
		Source []string `configfile:"true" boa:"configonly" json:"-"`
		Name   string   `optional:"true" json:"name"`
	}
	type Params struct {
		Source string `configfile:"true" boa:"configonly" json:"-"`
		Auth   Auth   `json:"auth"`
	}
	base := writeTestConfigFile(t, `{"name":"base"}`)
	overlay := writeTestConfigFile(t, `{"name":"overlay","unknown":true}`)
	p := Params{Source: writeTestConfigFile(t, `{"auth":{"name":"root"}}`), Auth: Auth{Source: []string{base, overlay}}}
	err := (Cmd[Params]{Params: &p, RawArgs: []string{}, RejectUnknown: true}).Validate()
	if err == nil || !strings.Contains(err.Error(), overlay) || !strings.Contains(err.Error(), "unknown") || p.Auth.Name != "base" {
		t.Fatalf("invalid overlay must not decode; previous file stays applied: %+v, %v", p, err)
	}
	p = Params{Source: writeTestConfigFile(t, `{"auth":{"name":"root","unknown":true}}`), Auth: Auth{Source: []string{base}}}
	if err := (Cmd[Params]{Params: &p, RawArgs: []string{}, RejectUnknown: true}).Validate(); err == nil || !strings.Contains(err.Error(), "auth.unknown") {
		t.Fatalf("root file must also be strict: %v", err)
	}
}

func TestRejectUnknown_Reload(t *testing.T) {
	path := writeTestConfigFile(t, `{"Host":"old"}`)
	cmd := Cmd[reloadTestParams]{Use: "test", RejectUnknown: true,
		RunFuncCtx: func(ctx *HookContext, p *reloadTestParams, _ *cobra.Command, _ []string) {
			if err := os.WriteFile(path, []byte(`{"Host":"new","unknown":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			fresh, err := Reload[reloadTestParams](ctx)
			if err == nil || !IsUserInputError(err) || fresh != nil || p.Host != "old" {
				t.Fatalf("invalid reload changed old snapshot: fresh=%+v old=%+v err=%v", fresh, p, err)
			}
		},
	}
	if err := cmd.RunArgsE([]string{"--config-file", path}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectUnknown_DoesNotChangeOtherDecoding(t *testing.T) {
	type Params struct {
		Source      string             `configfile:"true" optional:"true" json:"-"`
		Credentials []strictCredential `optional:"true" env:"BOA_STRICT_CREDENTIALS"`
	}
	payload := `[{"username":"a","unknown":true}]`
	for _, source := range []string{"cli", "env", "standalone", "permissive file"} {
		t.Run(source, func(t *testing.T) {
			p := Params{}
			cmd := Cmd[Params]{Params: &p, RawArgs: []string{}, RejectUnknown: true}
			switch source {
			case "cli":
				cmd.RawArgs = []string{"--credentials", payload}
			case "env":
				t.Setenv("BOA_STRICT_CREDENTIALS", payload)
			case "permissive file":
				p.Source = writeTestConfigFile(t, `{"Credentials":`+payload+`,"unknown":true}`)
				cmd.RejectUnknown = false
			case "standalone":
				if err := LoadConfigBytes([]byte(`{"Credentials":`+payload+`,"unknown":true}`), ".json", &p, nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Validate(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Credentials, []strictCredential{{Username: "a"}}) {
				t.Fatalf("source=%s p=%+v", source, p)
			}
		})
	}
	var p Params
	if err := LoadConfigFile(writeTestConfigFile(t, `{"unknown":true}`), &p, json.Unmarshal); err != nil {
		t.Fatalf("standalone file helper changed: %v", err)
	}
	if err := (Cmd[strictConfig]{RawArgs: []string{"--credentials", "[]"}, RejectUnknown: true}).Validate(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("configonly exposed a CLI flag: %v", err)
	}
}

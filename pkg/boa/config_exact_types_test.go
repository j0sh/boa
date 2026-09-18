package boa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type canonicalConfigString string

type parsedConfigPoint struct {
	X int
	Y int
}

func registerTypeCleanup[T any](t *testing.T, def TypeDef[T]) {
	t.Helper()
	typeOfT := reflect.TypeOf((*T)(nil)).Elem()
	previous, existed := exactTypeHandlers[typeOfT]
	RegisterType(def)
	t.Cleanup(func() {
		if existed {
			exactTypeHandlers[typeOfT] = previous
			return
		}
		delete(exactTypeHandlers, typeOfT)
	})
}

func TestConfigExactType_StringBackedRegistrationAlwaysParses(t *testing.T) {
	parseCalls := 0
	registerTypeCleanup(t, TypeDef[canonicalConfigString]{
		Parse: func(value string) (canonicalConfigString, error) {
			parseCalls++
			value = strings.ToLower(strings.TrimSpace(value))
			if !strings.HasPrefix(value, "cfg-") {
				return "", fmt.Errorf("must start with cfg-")
			}
			return canonicalConfigString(value), nil
		},
	})

	type Nested struct {
		Value canonicalConfigString `json:"value"`
	}
	type Params struct {
		Scalar  canonicalConfigString            `json:"scalar"`
		Pointer *canonicalConfigString           `json:"pointer"`
		Nested  Nested                           `json:"nested"`
		Values  []canonicalConfigString          `json:"values"`
		ByName  map[string]canonicalConfigString `json:"by_name"`
	}

	var params Params
	err := LoadConfigBytes([]byte(`{
		"scalar":" CFG-SCALAR ",
		"pointer":" CFG-POINTER ",
		"nested":{"value":" CFG-NESTED "},
		"values":[" CFG-A ","cfg-b"],
		"by_name":{"first":" CFG-MAP "}
	}`), ".json", &params, nil)
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}

	pointer := canonicalConfigString("cfg-pointer")
	want := Params{
		Scalar: "cfg-scalar", Pointer: &pointer, Nested: Nested{"cfg-nested"},
		Values: []canonicalConfigString{"cfg-a", "cfg-b"},
		ByName: map[string]canonicalConfigString{"first": "cfg-map"},
	}
	if !reflect.DeepEqual(params, want) || parseCalls != 6 {
		t.Fatalf("params=%+v Parse calls=%d, want %+v and 6 calls", params, parseCalls, want)
	}
	err = LoadConfigBytes([]byte(`{"scalar":"invalid"}`), ".json", &params, nil)
	if err == nil || !strings.Contains(err.Error(), "must start with cfg-") || !strings.Contains(err.Error(), "Scalar") {
		t.Fatalf("error = %v, want registered parser failure with field path", err)
	}
}

func TestConfigExactType_NonStringBackedRegistration(t *testing.T) {
	registerTypeCleanup(t, TypeDef[parsedConfigPoint]{
		Parse: func(value string) (parsedConfigPoint, error) {
			var point parsedConfigPoint
			if _, err := fmt.Sscanf(value, "%d:%d", &point.X, &point.Y); err != nil {
				return parsedConfigPoint{}, err
			}
			return point, nil
		},
	})

	var params struct {
		Name  string            `json:"name"`
		Point parsedConfigPoint `json:"point"`
	}
	err := LoadConfigBytes([]byte(`{"name":"kept","point":"3:7"}`), ".json", &params, nil)
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if params.Name != "kept" || params.Point != (parsedConfigPoint{X: 3, Y: 7}) {
		t.Fatalf("params = %+v, want Name=kept Point={3 7}", params)
	}
}

func TestConfigExactType_DurationShapesAndNumericCompatibility(t *testing.T) {
	type Nested struct {
		Delay time.Duration `json:"delay"`
	}
	type Params struct {
		Timeout   time.Duration            `json:"timeout"`
		Pointer   *time.Duration           `json:"pointer"`
		Nested    Nested                   `json:"nested"`
		Delays    []time.Duration          `json:"delays"`
		ByName    map[string]time.Duration `json:"by_name"`
		Numeric   time.Duration            `json:"numeric"`
		Untouched string                   `json:"untouched"`
	}

	var params Params
	err := LoadConfigBytes([]byte(`{
		"timeout":"1h30m",
		"pointer":"250ms",
		"nested":{"delay":"-1.5s"},
		"delays":["1s","2m"],
		"by_name":{"fast":"10ms"},
		"numeric":1500000000,
		"untouched":"normal"
	}`), ".json", &params, nil)
	if err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}

	pointer := 250 * time.Millisecond
	want := Params{
		Timeout: 90 * time.Minute, Pointer: &pointer, Nested: Nested{-1500 * time.Millisecond},
		Delays:  []time.Duration{time.Second, 2 * time.Minute},
		ByName:  map[string]time.Duration{"fast": 10 * time.Millisecond},
		Numeric: 1500 * time.Millisecond, Untouched: "normal",
	}
	if !reflect.DeepEqual(params, want) {
		t.Fatalf("params = %+v, want %+v", params, want)
	}
}

func TestConfigExactType_DurationInEmbeddedStruct(t *testing.T) {
	type Common struct {
		Timeout time.Duration
	}

	t.Run("promoted field", func(t *testing.T) {
		var params struct{ Common }
		if err := LoadConfigBytes([]byte(`{"timeout":"2.5h"}`), ".json", &params, nil); err != nil {
			t.Fatalf("LoadConfigBytes: %v", err)
		}
		if params.Timeout != 150*time.Minute {
			t.Fatalf("Timeout = %v, want 2.5h", params.Timeout)
		}
	})

	t.Run("promoted field and sibling", func(t *testing.T) {
		var params struct {
			Common
			Delay time.Duration `json:"delay"`
		}
		if err := LoadConfigBytes([]byte(`{"timeout":"2.5h","delay":"3s"}`), ".json", &params, nil); err != nil {
			t.Fatalf("LoadConfigBytes: %v", err)
		}
		if params.Timeout != 150*time.Minute || params.Delay != 3*time.Second {
			t.Fatalf("params = %+v, want Timeout=2.5h Delay=3s", params)
		}
	})
}

func TestConfigExactType_DurationRejectsInvalidValues(t *testing.T) {
	strict := func(data []byte, target any) error {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		return decoder.Decode(target)
	}
	for _, value := range []string{`"tomorrow"`, "true", "{}", "[]", "1.5", "1.0", "1e3", "1.0000000000000000001", "9223372036854775808", "-9223372036854775809"} {
		for _, shape := range []string{
			`{"timeout":%s}`,
			`{"pointer":%s}`,
			`{"nested":{"timeout":%s}}`,
			`{"delays":["1s",%s]}`,
			`{"by_name":{"string":"1s","numeric":%s}}`,
			`{"timeout":"1h","quoted":%s}`,
			`{"timeout":"1h","count":%s}`,
			`{"timeout":"1h","unknown":%s}`,
		} {
			data := fmt.Sprintf(shape, value)
			t.Run(data, func(t *testing.T) {
				var params struct {
					Timeout time.Duration  `json:"timeout"`
					Quoted  time.Duration  `json:"quoted,string"`
					Count   int            `json:"count"`
					Pointer *time.Duration `json:"pointer"`
					Nested  struct {
						Timeout time.Duration `json:"timeout"`
					} `json:"nested"`
					Delays []time.Duration          `json:"delays"`
					ByName map[string]time.Duration `json:"by_name"`
				}
				if err := LoadConfigBytes([]byte(data), ".json", &params, strict); err == nil {
					t.Fatal("expected invalid duration error")
				}
			})
		}
	}
}

func TestConfigExactType_NativeValuesAfterEarlyDecoderError(t *testing.T) {
	type Params struct {
		At      time.Time                `json:"at"`
		Delay   time.Duration            `json:"delay"`
		Pointer *time.Duration           `json:"pointer"`
		Delays  []time.Duration          `json:"delays"`
		ByName  map[string]time.Duration `json:"by_name"`
	}
	for _, numeric := range []string{"5000000000", "9007199254740993", "9223372036854775807", "-9223372036854775808"} {
		t.Run(numeric, func(t *testing.T) {
			var want time.Duration
			if err := json.Unmarshal([]byte(numeric), &want); err != nil {
				t.Fatal(err)
			}
			var params Params
			data := fmt.Sprintf(`{"at":"2026-09-17","delay":%[1]s,"pointer":%[1]s,"delays":["1s",%[1]s],"by_name":{"string":"1s","numeric":%[1]s}}`, numeric)
			if err := LoadConfigBytes([]byte(data), ".json", &params, nil); err != nil {
				t.Fatalf("LoadConfigBytes: %v", err)
			}
			if !params.At.Equal(time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)) || params.Delay != want || params.Pointer == nil || *params.Pointer != want || !reflect.DeepEqual(params.Delays, []time.Duration{time.Second, want}) || !reflect.DeepEqual(params.ByName, map[string]time.Duration{"string": time.Second, "numeric": want}) {
				t.Fatalf("params = %+v, want date and native duration %d", params, want)
			}
		})
	}
}

func TestConfigExactType_OverlayPreservesOmittedValues(t *testing.T) {
	type Params struct {
		Timeout time.Duration `json:"timeout"`
		Name    string        `json:"name"`
	}
	params := Params{Name: "initial"}
	if err := LoadConfigBytes([]byte(`{"timeout":"1m","name":"base"}`), ".json", &params, nil); err != nil {
		t.Fatalf("base LoadConfigBytes: %v", err)
	}
	if err := LoadConfigBytes([]byte(`{"timeout":"2m"}`), ".json", &params, nil); err != nil {
		t.Fatalf("overlay LoadConfigBytes: %v", err)
	}
	if params.Timeout != 2*time.Minute || params.Name != "base" {
		t.Fatalf("params = %+v, want Timeout=2m Name=base", params)
	}
}

func TestConfigExactType_AutomaticConfigAndCLIPrecedence(t *testing.T) {
	type Params struct {
		ConfigFile string        `configfile:"true" optional:"true"`
		Timeout    time.Duration `json:"timeout" optional:"true"`
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"timeout":"1m"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want time.Duration
	}{
		{"config string is loaded and tracked", nil, time.Minute},
		{"CLI overrides config", []string{"--timeout", "2m"}, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got time.Duration
			var hasValue bool
			err := (CmdT[Params]{
				Use: "test",
				RunFuncCtx: func(ctx *HookContext, params *Params, _ *cobra.Command, _ []string) {
					got, hasValue = params.Timeout, ctx.HasValue(&params.Timeout)
				},
			}).RunArgsE(append([]string{"--config-file", configPath}, tc.args...))
			if err != nil || got != tc.want || !hasValue {
				t.Fatalf("Timeout=%v HasValue=%v error=%v, want %v/true/nil", got, hasValue, err, tc.want)
			}
		})
	}
}

func TestConfigExactType_CustomFormatNeedsNoMarshaler(t *testing.T) {
	registerFormatCleanup(t, ".typed", UniversalConfigFormat(func(data []byte, target any) error {
		// A raw JSON retry would fail: every document must have this prefix.
		payload, ok := strings.CutPrefix(string(data), "typed\n")
		if !ok {
			return fmt.Errorf("missing typed format prefix")
		}
		decoder := json.NewDecoder(strings.NewReader(payload))
		decoder.UseNumber()
		return decoder.Decode(target)
	}))
	registerTypeCleanup(t, TypeDef[canonicalConfigString]{
		Parse: func(value string) (canonicalConfigString, error) {
			return canonicalConfigString(strings.ToUpper(value)), nil
		},
	})

	var params struct {
		Value  canonicalConfigString `typed:"value" json:"value"`
		Delay  time.Duration         `typed:"delay" json:"delay"`
		At     time.Time             `typed:"at" json:"at"`
		Native time.Duration         `typed:"native" json:"native"`
	}
	if err := LoadConfigBytes([]byte("typed\n"+`{"value":"canonical","delay":"3s","at":"2026-09-17","native":9007199254740993}`), ".typed", &params, nil); err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if params.Value != "CANONICAL" || params.Delay != 3*time.Second || params.Native != 9007199254740993 {
		t.Fatalf("params = %+v, want Value=CANONICAL Delay=3s Native=9007199254740993", params)
	}
}

func TestConfigExactType_KeyTreePlaceholdersAreNotValues(t *testing.T) {
	registerFormatCleanup(t, ".placeholder", ConfigFormat{
		Unmarshal: json.Unmarshal,
		KeyTree: func([]byte) (map[string]any, error) {
			return map[string]any{
				"value":  "placeholder",
				"values": []any{"placeholder"},
			}, nil
		},
	})
	registerTypeCleanup(t, TypeDef[canonicalConfigString]{
		Parse: func(value string) (canonicalConfigString, error) {
			return canonicalConfigString(strings.ToUpper(value)), nil
		},
	})

	var params struct {
		Value  canonicalConfigString   `json:"value"`
		Values []canonicalConfigString `json:"values"`
	}
	if err := LoadConfigBytes([]byte(`{"value":"source","values":["one","two"]}`), ".placeholder", &params, nil); err != nil {
		t.Fatalf("LoadConfigBytes: %v", err)
	}
	if params.Value != "SOURCE" || !reflect.DeepEqual(params.Values, []canonicalConfigString{"ONE", "TWO"}) {
		t.Fatalf("params = %+v, want source values parsed independently of KeyTree", params)
	}
}

func TestConfigExactType_ConcreteOnlyParserKeepsOriginalError(t *testing.T) {
	type Params struct {
		Delay time.Duration `json:"delay"`
	}
	concreteOnly := func(data []byte, target any) error {
		params, ok := target.(*Params)
		if !ok {
			return fmt.Errorf("concrete-only target: got %T", target)
		}
		return json.Unmarshal(data, params)
	}

	var params Params
	err := LoadConfigBytes([]byte(`{"delay":"4s"}`), ".concrete", &params, concreteOnly)
	if err == nil {
		t.Fatal("expected concrete-only parser error")
	}
	if strings.Contains(err.Error(), "config exact-type proxy") || !strings.Contains(err.Error(), "cannot unmarshal string") {
		t.Fatalf("error = %q, want original format error", err)
	}
}

func TestConfigExactType_DurationDumpRemainsNumeric(t *testing.T) {
	params := struct {
		Timeout time.Duration `json:"timeout"`
	}{Timeout: 90 * time.Second}
	data, err := DumpConfigBytes(&params, ".json", nil)
	if err != nil {
		t.Fatalf("DumpConfigBytes: %v", err)
	}
	if strings.Contains(string(data), `"1m30s"`) || !strings.Contains(string(data), `90000000000`) {
		t.Fatalf("dump = %s, want numeric nanoseconds", data)
	}
}

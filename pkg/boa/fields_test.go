package boa

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type memoryOnly struct {
	Name string `default:"fresh"`
}

func (*memoryOnly) Init() error        { panic("standalone operation called Init") }
func (*memoryOnly) PreValidate() error { panic("standalone operation called PreValidate") }

type memoryScalar string

func (s *memoryScalar) UnmarshalText([]byte) error {
	panic("standalone validation decoded a value")
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func failures(t *testing.T, err error, want ...string) ValidationError {
	t.Helper()
	var result *ValidationError
	if !errors.As(err, &result) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if want != nil {
		var got []string
		for _, field := range result.Fields {
			got = append(got, field.Path+":"+string(field.Code))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("failures = %v, want %v", got, want)
		}
	}
	return *result
}

func TestFieldRulesCLIParity(t *testing.T) {
	type params struct {
		Count int           `default:"1" min:"1" max:"2"`
		Size  uint64        `default:"1" min:"1" max:"18446744073709551614"`
		Name  string        `default:"ok" pattern:"^[a-z]+$"`
		Mode  displayMode   `default:"tcp" alts:"tcp,http"`
		Tags  []displayMode `default:"[tcp]" alts:"tcp,http"`
		Extra string        `default:"any" alts:"tcp,http" strict:"false"`
	}
	valid, err := Defaults[params]()
	requireNoError(t, err)
	requireNoError(t, Validate(valid))
	cli := Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName, RunFunc: func(*params, *cobra.Command, []string) {}}
	requireNoError(t, cli.RunArgsE(nil))
	for _, tc := range []struct {
		path string
		code ValidationCode
		args []string
		edit func(*params)
	}{
		{"Count", CodeMin, []string{"--count", "0"}, func(p *params) { p.Count = 0 }},
		{"Count", CodeMax, []string{"--count", "3"}, func(p *params) { p.Count = 3 }},
		{"Size", CodeMin, []string{"--size", "0"}, func(p *params) { p.Size = 0 }},
		{"Size", CodeMax, []string{"--size", "18446744073709551615"}, func(p *params) { p.Size = ^uint64(0) }},
		{"Name", CodePattern, []string{"--name", "NO"}, func(p *params) { p.Name = "NO" }},
		{"Mode", CodeAlternatives, []string{"--mode", "bad"}, func(p *params) { p.Mode = "bad" }},
		{"Tags[1]", CodeAlternatives, []string{"--tags", "tcp,bad"}, func(p *params) { p.Tags = []displayMode{"tcp", "bad"} }},
	} {
		t.Run(tc.path+string(tc.code), func(t *testing.T) {
			p := valid
			tc.edit(&p)
			field := failures(t, Validate(&p), tc.path+":"+string(tc.code)).Fields[0]
			err := cli.RunArgsE(tc.args)
			name := camelToKebabCase(strings.Split(tc.path, "[")[0])
			if want := "invalid value for param '" + name + "': " + field.Err.Error(); err == nil || err.Error() != want {
				t.Fatalf("CLI = %v, want %s", err, want)
			}
		})
	}
}

func TestValidatePresence(t *testing.T) {
	type group struct {
		Port int `min:"1"`
	}
	type params struct {
		Zero     int    `default:"10"`
		False    bool   `default:"true"`
		Empty    string `default:"fallback"`
		Port     *int   `min:"1"`
		Group    *group
		Required *int              `required:"true" default:"2"`
		Items    []string          `required:"true" min:"1"`
		Labels   map[string]string `required:"true" min:"1"`
	}
	p := params{}
	failures(t, Validate(&p), "Required:required", "Items:required", "Labels:required")
	zero := 0
	p.Required, p.Port, p.Group = &zero, &zero, &group{}
	p.Items, p.Labels = []string{}, map[string]string{}
	failures(t, Validate(&p), "Port:min", "Group.Port:min", "Items:min", "Labels:min")
	if p.Zero != 0 || p.False || p.Empty != "" {
		t.Fatal("validation applied defaults")
	}
}

func TestDefaultsIndependent(t *testing.T) {
	type name string
	type child struct {
		N int `default:"7"`
	}
	type params struct {
		Child    child
		Optional *child
		Names    []name  `default:"[a,b]"`
		Children []child `default:"[{\"N\":0}]"`
		Values   map[string][]int
	}
	source := map[string][]int{"a": {1}}
	configure := func(path string, p Parameter) {
		if path == "Values" {
			p.SetDefault(source)
		}
	}
	a, err := Defaults[params](configure)
	requireNoError(t, err)
	b, err := Defaults[params](configure)
	requireNoError(t, err)
	a.Names[0], a.Values["a"][0] = "changed", 9
	if !reflect.DeepEqual(b.Names, []name{"a", "b"}) || b.Values["a"][0] != 1 || source["a"][0] != 1 {
		t.Fatal("defaults aliased")
	}
	if b.Child.N != 7 || b.Optional != nil || b.Children[0].N != 0 {
		t.Fatalf("defaults overlaid: %+v", b)
	}
}

func TestValidateCollectionsAndLimits(t *testing.T) {
	type child struct {
		Port int `min:"1"`
	}
	type params struct {
		Matrix  [][]*child
		Groups  map[string]child
		Array   [1]child
		Dynamic map[string]any
	}
	p := params{Matrix: [][]*child{{{}}}, Groups: map[string]child{"b": {}, "a": {}}, Dynamic: map[string]any{"x": child{}}}
	failures(t, Validate(p), "Matrix[0][0].Port:min", `Groups["a"].Port:min`, `Groups["b"].Port:min`, "Array[0].Port:min", `Dynamic["x"].Port:min`)
	large := struct{ Rows []child }{Rows: make([]child, MaxValidationErrors+10)}
	err := failures(t, Validate(large))
	if len(err.Fields) != MaxValidationErrors || !err.Truncated || err.Fields[99].Path != "Rows[99].Port" {
		t.Fatalf("unbounded: %+v", err)
	}
}

func TestValidateCustomAndNonMutation(t *testing.T) {
	type child struct {
		Port int `min:"1"`
	}
	type params struct {
		Values map[string][]int
		Child  *child
		Name   string
	}
	p := params{Values: map[string][]int{"a": {1}}, Child: &child{}, Name: "old"}
	sentinel := errors.New("custom failure")
	configure := func(path string, param Parameter) {
		switch path {
		case "Values":
			(&Field[map[string][]int]{Parameter: param}).SetCustomValidator(func(v map[string][]int) error { v["a"][0] = 9; return sentinel })
		case "Child":
			(&Field[*child]{Parameter: param}).SetCustomValidator(func(v *child) error { v.Port = 80; return nil })
		case "Name":
			param.SetAlternatives([]string{"allowed"})
		}
	}
	err := Validate(&p, configure)
	failures(t, err, "Values:custom", "Child.Port:min", "Name:alternatives")
	if !errors.Is(err, sentinel) {
		t.Fatal("lost custom cause", err)
	}
	var field *FieldError
	if !errors.As(err, &field) || field.Path != "Values" {
		t.Fatal("missing field error")
	}
	if p.Values["a"][0] != 1 || p.Child.Port != 0 || p.Name != "old" {
		t.Fatalf("mutated: %+v", p)
	}
	type scalar struct {
		N int `default:"4"`
	}
	set := func(param Parameter) {
		param.SetMin(5)
		(&Field[int]{Parameter: param}).SetCustomValidator(func(n int) error {
			if n == 6 {
				return sentinel
			}
			return nil
		})
	}
	for _, n := range []int{4, 6} {
		standalone := failures(t, Validate(scalar{N: n}, func(_ string, p Parameter) { set(p) })).Fields[0]
		cliErr := (Cmd[scalar]{Use: "test", ParamEnrich: ParamEnricherName,
			InitFuncCtx: func(ctx *HookContext, p *scalar, _ *cobra.Command) error { set(Param(ctx, &p.N).Parameter); return nil },
			RunFunc:     func(*scalar, *cobra.Command, []string) {}}).RunArgsE([]string{"--n", strconv.Itoa(n)})
		if cliErr == nil || cliErr.Error() != "invalid value for param 'n': "+standalone.Err.Error() {
			t.Fatalf("enriched parity: %v", cliErr)
		}
	}
}

func TestStandaloneNoAutomaticIO(t *testing.T) {
	if _, err := Defaults[memoryOnly](); err != nil {
		t.Fatal(err)
	}
	requireNoError(t, Validate(memoryOnly{}))
	requireNoError(t, Validate(struct {
		Value memoryScalar `default:"unparsed"`
	}{}))
	type params struct {
		Host      string `env:"BOA_MEMORY_HOST" default:"default"`
		File      string `file:"true" default:"/boa/does-not-exist"`
		Config    string `configfile:"true" default:"/boa/does-not-exist.json"`
		Dir       string `basedir:"required,autocreate" default:"/boa/does-not-exist-dir"`
		Token     string `secret:"true" env:"BOA_MEMORY_TOKEN"`
		TokenFile string `secretfor:"Token" default:"/boa/does-not-exist-secret"`
		Bind      HTTPBind
	}
	t.Setenv("BOA_MEMORY_HOST", "environment")
	t.Setenv("BOA_MEMORY_TOKEN", "secret")
	p, err := Defaults[params]()
	if err != nil || p.Host != "default" || p.Token != "" {
		t.Fatalf("defaults sourced: %+v, %v", p, err)
	}
	requireNoError(t, p.Bind.UnmarshalText([]byte("0.0.0.0:8080")))
	requireNoError(t, Validate(&p))
	if p.Host != "default" || p.Token != "" {
		t.Fatal("validation sourced values")
	}
}

func TestFieldSchemaErrors(t *testing.T) {
	for _, input := range []any{nil, 1, (*struct{})(nil), struct {
		N int `min:"oops"`
	}{}, struct {
		N int `required:"oops"`
	}{}} {
		if failures(t, Validate(input)).Fields[0].Code != CodeSchema {
			t.Fatal("expected schema error")
		}
	}
	if _, err := Defaults[int](); err == nil {
		t.Fatal("expected nonstruct error")
	}
}

type displayMode string

func (m displayMode) String() string { return "display:" + string(m) }

func TestStandaloneCopy(t *testing.T) {
	if os.Getenv("BOA_COPY_TEST") == "" {
		// Fresh time.Local and an isolated stack limit make copy failures observable.
		cmd := exec.Command(os.Args[0], "-test.run=^TestStandaloneCopy$")
		cmd.Env = append(os.Environ(), "TZ=America/Los_Angeles", "BOA_COPY_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, output)
		}
		return
	}
	type params struct{ At time.Time }
	original := time.Unix(0, 0)
	value, err := Defaults[params](func(_ string, p Parameter) { p.SetDefault(original) })
	requireNoError(t, err)
	copies := []time.Time{value.At}
	requireNoError(t, (Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, p *params, _ *cobra.Command) error {
			Param(ctx, &p.At).SetDefault(original)
			return nil
		},
		RunFunc: func(p *params, _ *cobra.Command, _ []string) { copies = append(copies, p.At) },
	}).RunArgsE(nil))
	requireNoError(t, Validate(params{At: original}, func(_ string, p Parameter) {
		(&Field[time.Time]{Parameter: p}).SetCustomValidator(func(v time.Time) error { copies = append(copies, v); return nil })
	}))
	// Inspect only after all three operations, keeping time.Local cold while copying.
	for i, v := range copies {
		name, offset := v.Zone()
		if name != "PST" || offset != -28800 || v != original {
			t.Fatalf("copy %d changed: zone %s, offset %d", i, name, offset)
		}
	}
	type node struct{ Next *node }
	deep := &node{}
	for range 10000 {
		deep = &node{Next: deep}
	}
	debug.SetMaxStack(1 << 20)
	got := failures(t, Validate(deep, func(_ string, p Parameter) { p.SetCustomValidator(func(any) error { return nil }) })).Fields
	if len(got) != 1 || got[0].Code != CodeLimit {
		t.Fatalf("expected bounded copy: %+v", got)
	}
}

func TestValidateCycle(t *testing.T) {
	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle
	calls := 0
	err := Validate(cycle, func(path string, p Parameter) {
		if strings.HasSuffix(path, "Next") {
			p.SetCustomValidator(func(any) error { calls++; return nil })
		}
	})
	if err != nil || calls != 1 {
		t.Fatalf("cycle was traversed repeatedly: %d calls, %v", calls, err)
	}
}

func TestValidateTraversalWork(t *testing.T) {
	type graph struct{ Left, Right *graph }
	shared := &graph{}
	for range 30 {
		shared = &graph{Left: shared, Right: shared}
	}
	calls := 0
	fields := failures(t, Validate(shared, func(_ string, _ Parameter) { calls++ })).Fields
	if len(fields) != 1 || fields[0].Code != CodeLimit || calls > 2*maxFieldSteps {
		t.Fatalf("unbounded work: %d, %+v", calls, fields)
	}

	type child struct{ Value int }
	alias := &child{Value: 1}
	value := struct{ Left, Right *child }{alias, alias}
	fields = failures(t, Validate(value, func(path string, p Parameter) {
		if path == "Right.Value" {
			p.SetMin(2)
		}
	})).Fields
	if len(fields) != 1 || fields[0].Path != "Right.Value" {
		t.Fatalf("path-specific rules lost: %+v", fields)
	}

	requireNoError(t, Validate(struct {
		Bytes   []byte
		Strings []string
		Nested  map[string][]string
	}{
		make([]byte, 1<<20), make([]string, 1<<20), map[string][]string{"key": make([]string, 1<<20)},
	}))
}

func TestValidateRecursiveStepBoundary(t *testing.T) {
	type node struct {
		Value int
		Next  *node
	}
	for _, size := range []int{64, 65} {
		value := &node{Value: 1}
		for i := 1; i < size; i++ {
			value = &node{Value: 1, Next: value}
		}
		err := Validate(value)
		if size == 64 {
			if err != nil {
				t.Fatalf("below step limit: %v", err)
			}
		} else if failures(t, err).Fields[0].Code != CodeLimit {
			t.Fatalf("expected traversal/copy limit: %v", err)
		}
	}
}

func TestValidateBranchErrorsContinue(t *testing.T) {
	type node struct{ Next *node }
	deep := &node{}
	for range 10000 {
		deep = &node{Next: deep}
	}
	value := struct {
		Bad      map[int]int
		Dynamic  map[string]any
		Deep     *node
		Port     int   `min:"1"`
		Ignored  *node `boa:"ignore"`
		private  *node
		Disabled *node
	}{
		Bad: map[int]int{1: 1}, Dynamic: map[string]any{"bad": map[int]int{1: 1}}, Deep: deep, Ignored: deep, private: deep, Disabled: deep,
	}
	fields := failures(t, Validate(value, func(path string, p Parameter) {
		if path == "Disabled" {
			p.SetIsEnabledFn(func() bool { return false })
		}
	})).Fields
	if len(fields) != 4 || fields[0].Path != "Bad" || fields[0].Code != CodeSchema || fields[1].Path != `Dynamic["bad"]` || fields[1].Code != CodeSchema || fields[2].Code != CodeLimit || fields[3].Path != "Port" || fields[3].Code != CodeMin {
		t.Fatalf("missing siblings: %+v", fields)
	}
}

func TestCustomValidatorNamedTypeParity(t *testing.T) {
	type params struct {
		Mode     displayMode    `default:"tcp"`
		Ptr      *displayMode   `default:"tcp"`
		Modes    []displayMode  `default:"[tcp]"`
		PtrModes *[]displayMode `default:"[tcp]"`
	}
	sentinel := errors.New("expected tcp")
	check := func(text string) error {
		if text != "tcp" {
			return sentinel
		}
		return nil
	}
	for _, setup := range []ConfigureField{
		func(_ string, p Parameter) {
			p.SetCustomValidator(func(v any) error {
				switch v := v.(type) {
				case string:
					return check(v)
				case *string:
					return check(*v)
				case []string:
					return check(v[0])
				case *[]string:
					return check((*v)[0])
				default:
					return fmt.Errorf("unexpected normalized type %T", v)
				}
			})
		},
		func(path string, p Parameter) {
			switch path {
			case "Mode":
				(&Field[displayMode]{Parameter: p}).SetCustomValidator(func(v displayMode) error { return check(string(v)) })
			case "Ptr":
				(&Field[*displayMode]{Parameter: p}).SetCustomValidator(func(v *displayMode) error { return check(string(*v)) })
			case "Modes":
				(&Field[[]displayMode]{Parameter: p}).SetCustomValidator(func(v []displayMode) error { return check(string(v[0])) })
			case "PtrModes":
				(&Field[*[]displayMode]{Parameter: p}).SetCustomValidator(func(v *[]displayMode) error { return check(string((*v)[0])) })
			}
		},
	} {
		value, err := Defaults[params]()
		requireNoError(t, err)
		requireNoError(t, Validate(value, setup))
		value.Mode, *value.Ptr, value.Modes[0], (*value.PtrModes)[0] = "udp", "udp", "udp", "udp"
		fields := failures(t, Validate(value, setup), "Mode:custom", "Ptr:custom", "Modes:custom", "PtrModes:custom").Fields
		for _, field := range fields {
			if !errors.Is(field.Err, sentinel) {
				t.Fatal("lost cause", field)
			}
		}
		cli := Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
			InitFuncCtx: func(ctx *HookContext, p *params, _ *cobra.Command) error {
				setup("Mode", Param(ctx, &p.Mode).Parameter)
				setup("Ptr", Param(ctx, &p.Ptr).Parameter)
				setup("Modes", Param(ctx, &p.Modes).Parameter)
				setup("PtrModes", Param(ctx, &p.PtrModes).Parameter)
				return nil
			}, RunFunc: func(*params, *cobra.Command, []string) {},
		}
		requireNoError(t, cli.RunArgsE(nil))
		for _, path := range []string{"Mode", "Ptr", "Modes", "PtrModes"} {
			name := camelToKebabCase(path)
			if err := cli.RunArgsE([]string{"--" + name, "udp"}); err == nil || err.Error() != "invalid value for param '"+name+"': "+sentinel.Error() {
				t.Fatalf("%s CLI: %v", path, err)
			}
		}
	}
}

func TestParseConfigBytesManualPipeline(t *testing.T) {
	type config struct {
		Count   int    `default:"3" min:"0"`
		Enabled bool   `default:"true"`
		Name    string `default:"fresh"`
		Port    *int   `default:"80" min:"1"`
		Mode    string `default:"tcp" alts:"tcp,http"`
	}
	for i, data := range []string{`{}`, `{"Count":0,"Enabled":false,"Name":"","Port":null}`, `{"Count":-1,"Mode":"bad"}`} {
		manual, err := Defaults[config]()
		requireNoError(t, err)
		requireNoError(t, json.Unmarshal([]byte(data), &manual))
		manualErr := Validate(manual)
		parsed, err := ParseConfigBytes[config]([]byte(data), json.Unmarshal)
		if i == 0 && (parsed.Count != 3 || !parsed.Enabled || parsed.Name != "fresh" || parsed.Port == nil || *parsed.Port != 80) {
			t.Fatalf("missing defaults: %+v", parsed)
		}
		if i == 1 && (parsed.Count != 0 || parsed.Enabled || parsed.Name != "" || parsed.Port != nil) {
			t.Fatalf("explicit values lost: %+v", parsed)
		}
		if !reflect.DeepEqual(parsed, manual) || !reflect.DeepEqual(err, manualErr) {
			t.Fatalf("pipeline differs for %s: %+v, %v / %+v, %v", data, parsed, err, manual, manualErr)
		}
	}
	var decoded config
	requireNoError(t, LoadConfigBytes([]byte(`{}`), ".json", &decoded, nil))
	if !reflect.DeepEqual(decoded, config{}) {
		t.Fatal("LoadConfigBytes applied defaults")
	}
}

func TestParseConfigBytesErrors(t *testing.T) {
	type config struct {
		Count int `default:"3"`
	}
	sentinel := errors.New("decoder failure")
	value, err := ParseConfigBytes[config](nil, func(_ []byte, dst any) error { dst.(*config).Count = 4; return sentinel })
	if err != sentinel || value.Count != 4 {
		t.Fatalf("decoder error or partial value changed: %+v, %v", value, err)
	}
	if _, err := ParseConfigBytes[config](nil, nil); err == nil {
		t.Fatal("expected missing decoder error")
	}
	if _, err := ParseConfigBytes[struct {
		Count int `default:"bad"`
	}](nil, func([]byte, any) error { t.Fatal("decoded after default error"); return nil }); err == nil {
		t.Fatal("expected default error")
	}
}

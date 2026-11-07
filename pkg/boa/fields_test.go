package boa

import (
	"errors"
	"net/url"
	"reflect"
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

func failures(t *testing.T, err error) *ValidationError {
	t.Helper()
	var result *ValidationError
	if !errors.As(err, &result) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	return result
}

func TestFieldRulesCLIParity(t *testing.T) {
	type named string
	type params struct {
		Count int64             `default:"1" min:"1" max:"9007199254740993"`
		Size  uint64            `default:"1" min:"1" max:"18446744073709551614"`
		Ratio float64           `default:"1" min:"0.5" max:"2"`
		Wait  time.Duration     `default:"1s" min:"1" max:"2000000000"`
		Name  named             `default:"ok" min:"2" max:"4" pattern:"^[a-z]+$"`
		Mode  string            `default:"tcp" alts:"tcp,http"`
		Tags  []string          `default:"[tcp]" alts:"tcp,http" min:"1" max:"2"`
		Map   map[string]string `default:"a=b" min:"1" max:"2"`
		Extra string            `default:"any" alts:"tcp,http" strict:"false"`
	}
	cases := []struct {
		path string
		code ValidationCode
		args []string
		edit func(*params)
	}{
		{"Count", CodeMin, []string{"--count", "0"}, func(p *params) { p.Count = 0 }},
		{"Count", CodeMax, []string{"--count", "9007199254740994"}, func(p *params) { p.Count = 9007199254740994 }},
		{"Size", CodeMin, []string{"--size", "0"}, func(p *params) { p.Size = 0 }},
		{"Size", CodeMax, []string{"--size", "18446744073709551615"}, func(p *params) { p.Size = ^uint64(0) }},
		{"Ratio", CodeMin, []string{"--ratio", "0"}, func(p *params) { p.Ratio = 0 }},
		{"Ratio", CodeMax, []string{"--ratio", "3"}, func(p *params) { p.Ratio = 3 }},
		{"Wait", CodeMax, []string{"--wait", "3s"}, func(p *params) { p.Wait = 3 * time.Second }},
		{"Name", CodeMin, []string{"--name", "x"}, func(p *params) { p.Name = "x" }},
		{"Name", CodeMax, []string{"--name", "abcde"}, func(p *params) { p.Name = "abcde" }},
		{"Name", CodePattern, []string{"--name", "NO"}, func(p *params) { p.Name = "NO" }},
		{"Mode", CodeAlternatives, []string{"--mode", "bad"}, func(p *params) { p.Mode = "bad" }},
		{"Tags[1]", CodeAlternatives, []string{"--tags", "tcp,bad"}, func(p *params) { p.Tags = []string{"tcp", "bad"} }},
		{"Tags", CodeMax, []string{"--tags", "tcp,tcp,tcp"}, func(p *params) { p.Tags = []string{"tcp", "tcp", "tcp"} }},
		{"Map", CodeMax, []string{"--map", "a=b,c=d,e=f"}, func(p *params) { p.Map = map[string]string{"a": "b", "c": "d", "e": "f"} }},
	}
	for _, tc := range cases {
		t.Run(tc.path+string(tc.code), func(t *testing.T) {
			p, err := Defaults[params]()
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(p); err != nil {
				t.Fatal(err)
			}
			tc.edit(&p)
			field := failures(t, Validate(&p)).Fields[0]
			if field.Path != tc.path || field.Code != tc.code {
				t.Fatalf("unexpected failure: %+v", field)
			}
			cli := Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
				RunFunc: func(*params, *cobra.Command, []string) { t.Fatal("invalid input ran") }}
			err = cli.RunArgsE(tc.args)
			name := camelToKebabCase(strings.Split(tc.path, "[")[0])
			want := "invalid value for param '" + name + "': " + field.Err.Error()
			if err == nil || err.Error() != want {
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
	got := failures(t, Validate(&p)).Fields
	if len(got) != 3 || got[0].Path != "Required" || got[1].Path != "Items" || got[2].Path != "Labels" {
		t.Fatalf("unexpected absent failures: %+v", got)
	}
	for _, field := range got {
		if field.Code != CodeRequired {
			t.Fatalf("expected required: %+v", field)
		}
	}
	zero := 0
	p.Required, p.Port, p.Group = &zero, &zero, &group{}
	p.Items, p.Labels = []string{}, map[string]string{}
	got = failures(t, Validate(&p)).Fields
	if len(got) != 4 || got[0].Path != "Port" || got[1].Path != "Group.Port" {
		t.Fatalf("unexpected present failures: %+v", got)
	}
	for _, field := range got {
		if field.Code != CodeMin {
			t.Fatalf("expected min: %+v", field)
		}
	}
	if p.Zero != 0 || p.False || p.Empty != "" {
		t.Fatal("validation applied defaults")
	}
	type required struct {
		Port *int `required:"true" env:"BOA_PORT"`
	}
	cliErr := (Cmd[required]{Use: "test", ParamEnrich: ParamEnricherName,
		RunFunc: func(*required, *cobra.Command, []string) {}}).RunArgsE(nil)
	if cliErr == nil || cliErr.Error() != "missing required param 'port' (env: BOA_PORT)" {
		t.Fatal(cliErr)
	}
	if failures(t, Validate(required{})).Fields[0].Code != CodeRequired {
		t.Fatal("required parity")
	}
}

func TestDefaultsBeforeDecode(t *testing.T) {
	type child struct {
		Port int `default:"80"`
	}
	type params struct {
		Count    int    `default:"10"`
		Enabled  bool   `default:"true"`
		Name     string `default:"default"`
		Port     *int   `default:"90"`
		Child    child
		Optional *child
		Items    []string            `default:"[a,b]"`
		Labels   map[string][]string `default:"{\"a\":[\"b\"]}"`
	}
	a, err := Defaults[params]()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Defaults[params]()
	if err != nil {
		t.Fatal(err)
	}
	if a.Count != 10 || !a.Enabled || a.Name != "default" || *a.Port != 90 || a.Child.Port != 80 || a.Optional != nil {
		t.Fatalf("defaults: %+v", a)
	}
	a.Items[0], a.Labels["a"][0], *a.Port = "changed", "changed", 1
	if b.Items[0] != "a" || b.Labels["a"][0] != "b" || *b.Port != 90 {
		t.Fatal("defaults aliased")
	}
	if err := LoadConfigBytes([]byte(`{"Count":0,"Enabled":false,"Name":"","Port":null}`), ".json", &b, nil); err != nil {
		t.Fatal(err)
	}
	if b.Count != 0 || b.Enabled || b.Name != "" || b.Port != nil || b.Child.Port != 80 {
		t.Fatalf("explicit values lost: %+v", b)
	}
	var decoded params
	if err := LoadConfigBytes([]byte(`{}`), ".json", &decoded, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, params{}) {
		t.Fatal("LoadConfigBytes added defaults")
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
	got := failures(t, Validate(p)).Fields
	want := []string{"Matrix[0][0].Port", `Groups["a"].Port`, `Groups["b"].Port`, "Array[0].Port", `Dynamic["x"].Port`}
	var paths []string
	for _, field := range got {
		paths = append(paths, field.Path)
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
	large := struct{ Rows []child }{Rows: make([]child, MaxValidationErrors+10)}
	err := failures(t, Validate(large))
	if len(err.Fields) != MaxValidationErrors || !err.Truncated || err.Fields[99].Path != "Rows[99].Port" {
		t.Fatalf("unbounded: %+v", err)
	}
	type recursive struct {
		Value int `min:"1"`
		Next  *recursive
	}
	cycle := &recursive{Value: 1}
	cycle.Next = cycle
	if err := Validate(cycle); err != nil {
		t.Fatal(err)
	}
	deep := &recursive{Value: 1}
	for i := 0; i < 130; i++ {
		deep = &recursive{Value: 1, Next: deep}
	}
	if failures(t, Validate(deep)).Fields[0].Code != CodeLimit {
		t.Fatal("expected depth limit")
	}
}

func TestValidateCustomAndNonMutation(t *testing.T) {
	type params struct {
		Values map[string][]int
		Port   *int
		Name   string
	}
	port := 7
	p := params{Values: map[string][]int{"a": {1}}, Port: &port, Name: "old"}
	sentinel := errors.New("custom failure")
	configure := func(path string, param Parameter) {
		switch path {
		case "Values":
			(&Field[map[string][]int]{Parameter: param}).SetCustomValidator(func(v map[string][]int) error { v["a"][0] = 9; return sentinel })
		case "Port":
			(&Field[*int]{Parameter: param}).SetCustomValidator(func(v *int) error { *v = 9; return sentinel })
		case "Name":
			param.SetAlternatives([]string{"allowed"})
		}
	}
	err := Validate(&p, configure)
	got := failures(t, err).Fields
	if len(got) != 3 || got[0].Code != CodeCustom || got[1].Code != CodeCustom || got[2].Code != CodeAlternatives || !errors.Is(err, sentinel) {
		t.Fatalf("custom errors: %v", got)
	}
	var field *FieldError
	if !errors.As(err, &field) || field.Path != "Values" {
		t.Fatal("missing field error")
	}
	if p.Values["a"][0] != 1 || port != 7 || p.Name != "old" {
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
	if err := Validate(memoryOnly{}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(struct {
		Value memoryScalar `default:"unparsed"`
	}{}); err != nil {
		t.Fatal(err)
	}
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
	if err := p.Bind.UnmarshalText([]byte("0.0.0.0:8080")); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&p); err != nil {
		t.Fatal(err)
	}
	if p.Host != "default" || p.Token != "" {
		t.Fatal("validation sourced values")
	}
	type opaque struct {
		URL *url.URL `default:"https://example.com"`
	}
	p2, err := Defaults[opaque]()
	if err != nil || p2.URL.Host != "example.com" {
		t.Fatalf("pointer scalar default: %v", err)
	}
	if err := Validate(p2); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsConfiguredAndNamed(t *testing.T) {
	type name string
	type child struct {
		N int `default:"7"`
	}
	type params struct {
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
	if err != nil {
		t.Fatal(err)
	}
	b, err := Defaults[params](configure)
	if err != nil {
		t.Fatal(err)
	}
	a.Values["a"][0] = 9
	if b.Values["a"][0] != 1 || source["a"][0] != 1 {
		t.Fatal("configured defaults aliased")
	}
	if !reflect.DeepEqual(a.Names, []name{"a", "b"}) || a.Children[0].N != 0 {
		t.Fatalf("typed defaults overlaid: %+v", a)
	}
	if err := Validate(struct {
		N int `min:"1"`
	}{}, func(_ string, p Parameter) { p.SetIsEnabledFn(func() bool { return false }) }); err != nil {
		t.Fatal(err)
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
	if _, err := Defaults[struct {
		N int `default:"oops"`
	}](); err == nil {
		t.Fatal("expected default error")
	}
	if err := Validate(struct {
		N int `default:"oops"`
	}{}); err != nil {
		t.Fatal("validation parsed defaults", err)
	}
	type ignored struct {
		N       int `boa:"ignore" min:"1"`
		Private int `min:"1"`
		hidden  int `min:"1"`
	}
	if err := Validate(ignored{hidden: 0}, func(_ string, p Parameter) { p.SetIgnored(true) }); err != nil {
		t.Fatal(err)
	}
}

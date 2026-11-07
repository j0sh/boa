package boa

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type displayMode string

func (m displayMode) String() string { return "display:" + string(m) }

func TestAlternativesNamedDisplayParity(t *testing.T) {
	type params struct {
		Mode  displayMode   `alts:"tcp,http"`
		Modes []displayMode `alts:"tcp,http"`
	}
	for _, text := range []string{"tcp", "bad"} {
		value := params{Mode: displayMode(text), Modes: []displayMode{displayMode(text)}}
		standalone := Validate(value)
		cli := (Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
			RunFunc: func(*params, *cobra.Command, []string) {},
		}).RunArgsE([]string{"--mode", text, "--modes", text})
		if text == "tcp" {
			if standalone != nil || cli != nil {
				t.Fatalf("valid value: standalone %v, CLI %v", standalone, cli)
			}
			continue
		}
		fields := failures(t, standalone).Fields
		if len(fields) != 2 || fields[1].Path != "Modes[0]" || fields[0].Code != CodeAlternatives {
			t.Fatalf("unexpected errors: %+v", fields)
		}
		if cli == nil || cli.Error() != "invalid value for param 'mode': "+fields[0].Err.Error() {
			t.Fatalf("invalid value: standalone %v, CLI %v", standalone, cli)
		}
	}
}

func TestStandaloneCopyRegression(t *testing.T) {
	mode := os.Getenv("BOA_COPY_REGRESSION")
	if mode == "" {
		for _, mode := range []string{"defaults", "cli", "validate", "depth", "copy-depth"} {
			t.Run(mode, func(t *testing.T) {
				// A fresh process keeps time.Local uninitialized and isolates the
				// stack limit needed to catch cloning before bounded traversal.
				cmd := exec.Command(os.Args[0], "-test.run=^TestStandaloneCopyRegression$")
				cmd.Env = append(os.Environ(), "TZ=America/Los_Angeles", "BOA_COPY_REGRESSION="+mode)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v\n%s", err, output)
				}
			})
		}
		return
	}
	if mode == "depth" || mode == "copy-depth" {
		type node struct{ Next *node }
		value := &node{}
		for range 10000 {
			value = &node{Next: value}
		}
		debug.SetMaxStack(1 << 20)
		var err error
		if mode == "copy-depth" {
			err = Validate(value, func(_ string, p Parameter) { p.SetCustomValidator(func(any) error { return nil }) })
		} else {
			err = Validate(value)
		}
		if fields := failures(t, err).Fields; len(fields) != 1 || fields[0].Code != CodeLimit {
			t.Fatalf("expected bounded copy error: %+v", fields)
		}
		return
	}
	type params struct{ At time.Time }
	original := time.Unix(0, 0)
	check := func(value time.Time) error {
		name, offset := value.Zone()
		if name != "PST" || offset != -28800 || value != original {
			return fmt.Errorf("copied time changed: zone %s, offset %d", name, offset)
		}
		return nil
	}
	switch mode {
	case "defaults":
		value, err := Defaults[params](func(_ string, p Parameter) { p.SetDefault(original) })
		requireNoError(t, err)
		requireNoError(t, check(value.At))
	case "cli":
		err := (Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
			InitFuncCtx: func(ctx *HookContext, p *params, _ *cobra.Command) error {
				Param(ctx, &p.At).SetDefault(original)
				return nil
			},
			RunFuncE: func(p *params, _ *cobra.Command, _ []string) error { return check(p.At) },
		}).RunArgsE(nil)
		requireNoError(t, err)
	case "validate":
		err := Validate(params{At: original}, func(_ string, p Parameter) {
			(&Field[time.Time]{Parameter: p}).SetCustomValidator(check)
		})
		requireNoError(t, err)
	default:
		t.Fatalf("unknown regression mode %q", mode)
	}
}

func TestValidateExcludedDepth(t *testing.T) {
	type node struct{ Next *node }
	deep := &node{}
	for range 10000 {
		deep = &node{Next: deep}
	}
	value := struct {
		Ignored  *node `boa:"ignore"`
		private  *node
		Disabled *node
		Included int `min:"1"`
	}{Ignored: deep, private: deep, Disabled: deep, Included: 1}
	requireNoError(t, Validate(value, func(path string, p Parameter) {
		if path == "Disabled" {
			p.SetIsEnabledFn(func() bool { return false })
		}
	}))
}

func TestValidateCustomCopyTraversal(t *testing.T) {
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

	type child struct {
		Port int `min:"1"`
	}
	value := struct{ Child *child }{Child: &child{}}
	err = Validate(value, func(path string, p Parameter) {
		if path == "Child" {
			(&Field[*child]{Parameter: p}).SetCustomValidator(func(copy *child) error { copy.Port = 80; return nil })
		}
	})
	fields := failures(t, err).Fields
	if value.Child.Port != 0 || len(fields) != 1 || fields[0].Path != "Child.Port" || fields[0].Code != CodeMin {
		t.Fatalf("copy changed nested validation: %+v, %+v", value, fields)
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

	measure := func(size int) float64 {
		data := struct {
			Bytes   []byte
			Strings []string
			Nested  map[string][]string
		}{
			Bytes: make([]byte, size), Strings: make([]string, size), Nested: map[string][]string{"key": make([]string, size)},
		}
		return testing.AllocsPerRun(1, func() {
			requireNoError(t, Validate(data))
		})
	}
	if small, large := measure(1), measure(1<<20); large > small+20 {
		t.Fatalf("scalar collection allocations scale with size: %.0f -> %.0f", small, large)
	}
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
		cloner := configCloner{seen: make(map[configCopyKey]reflect.Value), maxDepth: maxFieldDepth}
		cloner.clone(reflect.ValueOf(value), 0)
		err := Validate(value)
		if size == 64 {
			if err != nil || cloner.exceeded {
				t.Fatalf("below step limit: %v", err)
			}
		} else if !cloner.exceeded || failures(t, err).Fields[0].Code != CodeLimit {
			t.Fatalf("expected traversal/copy limit: %v", err)
		}
	}
}

func TestValidateBranchErrorsContinue(t *testing.T) {
	type node struct{ Next *node }
	deep := &node{}
	for range 100 {
		deep = &node{Next: deep}
	}
	value := struct {
		Bad     map[int]int
		Dynamic map[string]any
		Deep    *node
		Port    int `min:"1"`
	}{
		Bad: map[int]int{1: 1}, Dynamic: map[string]any{"bad": map[int]int{1: 1}}, Deep: deep,
	}
	fields := failures(t, Validate(value)).Fields
	want := []struct {
		path string
		code ValidationCode
	}{{"Bad", CodeSchema}, {`Dynamic["bad"]`, CodeSchema}, {"", CodeLimit}, {"Port", CodeMin}}
	if len(fields) != len(want) {
		t.Fatalf("missing siblings: %+v", fields)
	}
	for i, w := range want {
		if fields[i].Code != w.code || w.path != "" && fields[i].Path != w.path {
			t.Fatalf("failure %d: %+v", i, fields[i])
		}
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
	for _, typed := range []bool{false, true} {
		setup := func(path string, p Parameter) {
			switch path {
			case "Mode":
				if typed {
					(&Field[displayMode]{Parameter: p}).SetCustomValidator(func(v displayMode) error { return check(string(v)) })
				} else {
					p.SetCustomValidator(func(v any) error { return check(v.(string)) })
				}
			case "Ptr":
				if typed {
					(&Field[*displayMode]{Parameter: p}).SetCustomValidator(func(v *displayMode) error { return check(string(*v)) })
				} else {
					p.SetCustomValidator(func(v any) error { return check(*v.(*string)) })
				}
			case "Modes":
				if typed {
					(&Field[[]displayMode]{Parameter: p}).SetCustomValidator(func(v []displayMode) error { return check(string(v[0])) })
				} else {
					p.SetCustomValidator(func(v any) error { return check(v.([]string)[0]) })
				}
			case "PtrModes":
				if typed {
					(&Field[*[]displayMode]{Parameter: p}).SetCustomValidator(func(v *[]displayMode) error { return check(string((*v)[0])) })
				} else {
					p.SetCustomValidator(func(v any) error { return check((*v.(*[]string))[0]) })
				}
			}
		}
		for _, path := range []string{"Mode", "Ptr", "Modes", "PtrModes"} {
			value, err := Defaults[params]()
			requireNoError(t, err)
			requireNoError(t, Validate(value, setup))
			switch path {
			case "Mode":
				value.Mode = "udp"
			case "Ptr":
				*value.Ptr = "udp"
			case "Modes":
				value.Modes[0] = "udp"
			case "PtrModes":
				(*value.PtrModes)[0] = "udp"
			}
			field := failures(t, Validate(value, setup)).Fields[0]
			if field.Path != path || field.Code != CodeCustom || !errors.Is(field.Err, sentinel) {
				t.Fatalf("bad typed=%v result: %+v", typed, field)
			}
			cli := Cmd[params]{Use: "test", ParamEnrich: ParamEnricherName,
				InitFuncCtx: func(ctx *HookContext, p *params, _ *cobra.Command) error {
					setup("Mode", Param(ctx, &p.Mode).Parameter)
					setup("Ptr", Param(ctx, &p.Ptr).Parameter)
					setup("Modes", Param(ctx, &p.Modes).Parameter)
					setup("PtrModes", Param(ctx, &p.PtrModes).Parameter)
					return nil
				},
				RunFunc: func(*params, *cobra.Command, []string) {},
			}
			requireNoError(t, cli.RunArgsE(nil))
			name := camelToKebabCase(path)
			if err := cli.RunArgsE([]string{"--" + name, "udp"}); err == nil || err.Error() != "invalid value for param '"+name+"': "+sentinel.Error() {
				t.Fatalf("typed=%v path=%s CLI: %v", typed, path, err)
			}
		}
	}
}

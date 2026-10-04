package boa

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
)

func assertUserInputError(t *testing.T, err, cause error, want ...string) {
	t.Helper()
	if !IsUserInputError(err) || cause != nil && !errors.Is(err, cause) {
		t.Fatalf("expected UserInputError wrapping %v, got: %v", cause, err)
	}
	for _, text := range want {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("expected error containing %q, got: %v", text, err)
		}
	}
}

func TestValidationTag_File(t *testing.T) {
	dir := t.TempDir()
	regular := writeFile(t, dir, "input", "{}")
	type pathCase struct {
		name, path, message string
		cause               error
	}
	paths := []pathCase{
		{"regular", regular, "", nil},
		{"missing", filepath.Join(dir, "missing"), "", os.ErrNotExist},
		{"directory", dir, "regular file", nil},
		{"invalid parent", filepath.Join(regular, "child"), "", syscall.ENOTDIR},
	}
	for _, tc := range paths[:3] {
		link := filepath.Join(dir, "link-"+tc.name)
		if err := os.Symlink(tc.path, link); err != nil {
			t.Logf("symlink unavailable: %v", err)
			continue
		}
		tc.name, tc.path = "symlink/"+tc.name, link
		paths = append(paths, tc)
	}
	t.Chdir(dir) // Keep the Unix socket name below platform path-length limits.
	if listener, err := net.Listen("unix", "socket"); err == nil {
		defer func() { _ = listener.Close() }()
		paths = append(paths, pathCase{"socket", "socket", "regular file", nil})
	} else {
		t.Logf("Unix socket unavailable: %v", err)
	}
	for _, tag := range []string{"file", "configfile"} {
		for _, option := range []string{"true", "optional", "false"} {
			t.Run(tag+"/"+option, func(t *testing.T) {
				typ := reflect.StructOf([]reflect.StructField{{Name: "Input", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(tag + `:"` + option + `"`)}})
				for _, tc := range paths {
					t.Run(tc.name, func(t *testing.T) {
						p := reflect.New(typ)
						err := (command{Params: p.Interface(), RawArgs: []string{"--input", tc.path}}).Validate()
						fail := option != "false" && (tc.cause != nil || tc.message != "") && (option == "true" || tc.cause != os.ErrNotExist)
						if fail {
							assertUserInputError(t, err, tc.cause, "input", tc.path, tc.message)
						} else if err != nil || p.Elem().Field(0).Elem().String() != tc.path {
							t.Fatalf("path=%s params=%v error=%v", tc.path, p, err)
						}
					})
				}
				baseDirOK(t, (command{Params: reflect.New(typ).Interface(), RawArgs: []string{}}).Validate())
			})
		}
		typ := reflect.StructOf([]reflect.StructField{{Name: "Input", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(tag + `:"optional"`)}})
		assertUserInputError(t, (command{Params: reflect.New(typ).Interface(), RawArgs: []string{}}).Validate(), nil, "missing required param 'input'")
	}
}

func TestFileTags_Declaration(t *testing.T) {
	for _, tag := range []string{"file", "configfile"} {
		for value, valid := range map[string]bool{
			"true,optional": true, " optional, true ": true,
			"optional-default": tag == "configfile", "true,optional-default": tag == "configfile",
			"": false, "yes": false, "true,": false, "true,true": false,
			"optional,optional": false, "false,true": false, "false,optional": false,
			"optional-default,optional-default": false, "false,optional-default": false, "optional,optional-default": false,
		} {
			typ := reflect.StructOf([]reflect.StructField{{Name: "Path", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(tag + `:"` + value + `"`)}})
			_, err := (command{Params: reflect.New(typ).Interface()}).ToCobraE()
			if (err == nil) != valid {
				t.Fatalf("%s:%q: %v", tag, value, err)
			}
			if err != nil && (IsUserInputError(err) || !strings.Contains(err.Error(), tag+" on field Path")) {
				t.Fatalf("expected %s declaration error for Path: %v", tag, err)
			}
		}
		for _, typ := range []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[[]int]()} {
			params := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "Path", Type: typ, Tag: reflect.StructTag(`name:"path" ` + tag + `:"optional"`)}})).Interface()
			want := "file tag requires a string field, got " + typ.String()
			if tag == "configfile" {
				want = "configfile on param path: must be a string or []string field"
			}
			if _, err := (command{Params: params}).ToCobraE(); err == nil || IsUserInputError(err) || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s type error for %s: %v", tag, typ, err)
			}
		}
	}
	type Slice struct {
		Input []string `file:"optional"`
	}
	if _, err := (Cmd[Slice]{}).ToCobraE(); err == nil || IsUserInputError(err) || !strings.Contains(err.Error(), "file tag requires a string field, got []string") {
		t.Fatalf("expected file type error for Input: %v", err)
	}
}

func TestFileMetadata_Overrides(t *testing.T) {
	type Params struct {
		Path   string `file:"optional" configfile:"optional-default" default:"default.json"`
		Config string `configfile:"optional" file:"true" optional:"true"`
	}
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name, path string
		set        func(Parameter)
		want       [5]bool // file, optional file, config, optional config, optional default
		fail       bool
	}{
		{"strict file", "default.json", func(p Parameter) { p.SetFileOptional(false) }, [5]bool{true, false, true, false, true}, true},
		{"disable file", "default.json", func(p Parameter) { p.SetFile(false) }, [5]bool{false, false, true, false, true}, false},
		{"reset file", "default.json", func(p Parameter) { p.SetFile(false); p.SetFile(true) }, [5]bool{true, false, true, false, true}, true},
		{"strict config", "default.json", func(p Parameter) { p.SetConfigFileOptionalDefault(false) }, [5]bool{true, true, true, false, false}, true},
		{"disable config", "other.json", func(p Parameter) { p.SetConfigFile(false) }, [5]bool{true, true, false, false, false}, false},
		{"reset config", "default.json", func(p Parameter) { p.SetConfigFile(false); p.SetConfigFile(true) }, [5]bool{true, true, true, false, false}, true},
		{"optional config", "other.json", func(p Parameter) { p.SetConfigFileOptional(true); p.SetConfigFileOptionalDefault(false) }, [5]bool{true, true, true, true, false}, false},
		{"optional default", "other.json", func(p Parameter) {
			p.SetConfigFileOptional(true)
			p.SetConfigFileOptionalDefault(true)
			p.SetConfigFileOptional(false)
		}, [5]bool{true, true, true, false, true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Cmd[Params]{RawArgs: []string{"--path", tc.path}, InitFuncCtx: func(ctx *HookContext, p *Params, _ *cobra.Command) error {
				path := Param(ctx, &p.Path)
				if !path.IsFile() || !path.IsFileOptional() || !path.IsConfigFile() || path.IsConfigFileOptional() || !path.IsConfigFileOptionalDefault() {
					t.Fatal("tags were not available during Init")
				}
				tc.set(path.Parameter)
				got := [5]bool{path.IsFile(), path.IsFileOptional(), path.IsConfigFile(), path.IsConfigFileOptional(), path.IsConfigFileOptionalDefault()}
				if got != tc.want {
					t.Fatalf("metadata=%v; want %v", got, tc.want)
				}
				return nil
			}}).Validate()
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v; want failure %v", err, tc.fail)
			}
			if tc.fail {
				assertUserInputError(t, err, os.ErrNotExist, "path", tc.path)
			}
		})
	}
	assertUserInputError(t, (Cmd[Params]{RawArgs: []string{"--config", "missing.json"}}).Validate(), os.ErrNotExist, "config", "missing.json")
}

// --- min/max for numeric types ---

func TestValidationTag_MinMax_Int(t *testing.T) {
	type Params struct {
		Port int `descr:"port" min:"1" max:"65535"`
	}

	// Valid value
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--port", "8080"})
	if err != nil {
		t.Fatalf("expected no error for valid port, got: %v", err)
	}

	// Below min
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--port", "0"})
	if err == nil {
		t.Fatal("expected error for port below min")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Errorf("expected error about min, got: %v", err)
	}

	// Above max
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--port", "70000"})
	if err == nil {
		t.Fatal("expected error for port above max")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Errorf("expected error about max, got: %v", err)
	}
}

func TestValidationTag_MinOnly(t *testing.T) {
	type Params struct {
		Count int `descr:"count" min:"0"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--count", "-1"})
	if err == nil {
		t.Fatal("expected error for count below min")
	}
}

func TestValidationTag_MaxOnly(t *testing.T) {
	type Params struct {
		Retries int `descr:"retries" max:"10"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--retries", "11"})
	if err == nil {
		t.Fatal("expected error for retries above max")
	}
}

func TestValidationTag_MinMax_Float(t *testing.T) {
	type Params struct {
		Rate float64 `descr:"rate" min:"0.0" max:"1.0"`
	}

	// Valid
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--rate", "0.5"})
	if err != nil {
		t.Fatalf("expected no error for valid rate, got: %v", err)
	}

	// Above max
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--rate", "1.5"})
	if err == nil {
		t.Fatal("expected error for rate above max")
	}
}

// --- pattern for string types ---

func TestValidationTag_Pattern(t *testing.T) {
	type Params struct {
		Name string `descr:"name" pattern:"^[a-z][a-z0-9-]*$"`
	}

	// Valid
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--name", "my-app-123"})
	if err != nil {
		t.Fatalf("expected no error for valid name, got: %v", err)
	}

	// Invalid — starts with uppercase
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--name", "MyApp"})
	if err == nil {
		t.Fatal("expected error for name not matching pattern")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("expected error about pattern, got: %v", err)
	}
}

func TestValidationTag_Pattern_Optional_NotSet(t *testing.T) {
	// Pattern should not trigger when the field is optional and not set
	type Params struct {
		Name *string `descr:"name" pattern:"^[a-z]+$"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err != nil {
		t.Fatalf("expected no error when optional pattern field not set, got: %v", err)
	}
}

// --- min/max for string length ---

func TestValidationTag_MinMax_Pointer_Set(t *testing.T) {
	// min/max should validate pointer fields when a value is provided
	type Params struct {
		Port *int `descr:"port" min:"1" max:"65535"`
	}

	// Valid
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--port", "8080"})
	if err != nil {
		t.Fatalf("expected no error for valid port, got: %v", err)
	}

	// Below min
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--port", "0"})
	if err == nil {
		t.Fatal("expected error for port below min")
	}
}

func TestValidationTag_MinMax_Pointer_NotSet(t *testing.T) {
	// min/max should NOT trigger when pointer field is not set (nil)
	type Params struct {
		Port *int `descr:"port" min:"1" max:"65535"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err != nil {
		t.Fatalf("expected no error when optional min/max field not set, got: %v", err)
	}
}

func TestValidationTag_Pattern_Pointer_Set(t *testing.T) {
	// pattern should validate pointer fields when a value is provided
	type Params struct {
		Tag *string `descr:"tag" pattern:"^v[0-9]+\\.[0-9]+\\.[0-9]+$"`
	}

	// Valid
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tag", "v1.2.3"})
	if err != nil {
		t.Fatalf("expected no error for valid tag, got: %v", err)
	}

	// Invalid
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tag", "latest"})
	if err == nil {
		t.Fatal("expected error for tag not matching pattern")
	}
}

func TestValidationTag_MinMax_StringLength(t *testing.T) {
	type Params struct {
		Name string `descr:"name" min:"3" max:"20"`
	}

	// Valid
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--name", "alice"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Too short
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--name", "ab"})
	if err == nil {
		t.Fatal("expected error for name too short")
	}

	// Too long
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--name", "a-very-long-name-that-exceeds-twenty"})
	if err == nil {
		t.Fatal("expected error for name too long")
	}
}

// --- min/max for slice length ---

func TestValidationTag_MinMax_Slice(t *testing.T) {
	type Params struct {
		Tags []string `descr:"tags" min:"1" max:"3"`
	}

	// Valid: 2 items
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tags", "a", "--tags", "b"})
	if err != nil {
		t.Fatalf("expected no error for valid slice, got: %v", err)
	}

	// Below min: 1 item when min is 1 (need at least 1 tag provided)
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err == nil {
		t.Fatal("expected error for slice below min (required with 0 items)")
	}

	// Above max: 4 items
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tags", "a", "--tags", "b", "--tags", "c", "--tags", "d"})
	if err == nil {
		t.Fatal("expected error for slice above max")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Errorf("expected error about max, got: %v", err)
	}
}

func TestValidationTag_MinOnly_Slice(t *testing.T) {
	type Params struct {
		Files []string `descr:"files" min:"2"`
	}

	// Valid: exactly 2
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--files", "a", "--files", "b"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Below min: 1 item
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--files", "a"})
	if err == nil {
		t.Fatal("expected error for slice below min")
	}
}

func TestValidationTag_MaxOnly_Slice(t *testing.T) {
	type Params struct {
		Items []string `descr:"items" max:"2" optional:"true"`
	}

	// Valid: 0 items
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err != nil {
		t.Fatalf("expected no error for empty slice, got: %v", err)
	}

	// Valid: 2 items
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--items", "a", "--items", "b"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Above max: 3 items
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--items", "a", "--items", "b", "--items", "c"})
	if err == nil {
		t.Fatal("expected error for slice above max")
	}
}

func TestValidationTag_MinMax_Slice_Positional(t *testing.T) {
	type Params struct {
		Files []string `positional:"true" min:"2" max:"4"`
	}

	// Valid: 3 items
	var got []string
	(Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) { got = p.Files },
	}).RunArgs([]string{"a", "b", "c"})
	if len(got) != 3 {
		t.Fatalf("expected 3 files, got %d", len(got))
	}

	// Below min: 1 item
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"a"})
	if err == nil {
		t.Fatal("expected error for positional slice below min")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Errorf("expected error about min, got: %v", err)
	}

	// Above max: 5 items
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"a", "b", "c", "d", "e"})
	if err == nil {
		t.Fatal("expected error for positional slice above max")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Errorf("expected error about max, got: %v", err)
	}
}

func TestValidationTag_MinMax_IntSlice(t *testing.T) {
	type Params struct {
		Ports []int `descr:"ports" min:"1" max:"3"`
	}

	// Valid: 2 items
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--ports", "80", "--ports", "443"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Above max: 4 items
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--ports", "80", "--ports", "443", "--ports", "8080", "--ports", "9090"})
	if err == nil {
		t.Fatal("expected error for int slice above max")
	}
}

func TestValidationTag_MinMax_RequiredSliceFlag(t *testing.T) {
	type Params struct {
		Tags []string `descr:"tags" min:"2" required:"true"`
	}

	// 0 items: required error fires first
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err == nil {
		t.Fatal("expected error for 0 items on required slice with min:2")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error for 0 items, got: %v", err)
	}

	// 1 item: min validation fires
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tags", "a"})
	if err == nil {
		t.Fatal("expected error for 1 item with min:2")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Errorf("expected 'min' error for 1 item, got: %v", err)
	}

	// 2 items: passes
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--tags", "a", "--tags", "b"})
	if err != nil {
		t.Fatalf("expected no error for 2 items, got: %v", err)
	}
}

func TestValidationTag_MinMax_RequiredSlicePositional(t *testing.T) {
	type Params struct {
		Files []string `positional:"true" min:"2" required:"true"`
	}

	// 0 items: cobra args validator fires first
	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{})
	if err == nil {
		t.Fatal("expected error for 0 positional args with min:2")
	}

	// 1 item: min validation fires
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"a"})
	if err == nil {
		t.Fatal("expected error for 1 positional arg with min:2")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Errorf("expected 'min' error, got: %v", err)
	}

	// 2 items: passes
	err = (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"a", "b"})
	if err != nil {
		t.Fatalf("expected no error for 2 items, got: %v", err)
	}
}

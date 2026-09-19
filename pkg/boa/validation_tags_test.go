package boa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestValidationTag_File(t *testing.T) {
	type Params struct {
		Input string `file:"true"`
	}

	run := func(path string) error {
		return (Cmd[Params]{
			Use:         "test",
			ParamEnrich: ParamEnricherName,
			RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
		}).RunArgsE([]string{"--input", path})
	}

	dir := t.TempDir()
	regular := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(regular, []byte("input"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	t.Run("regular file", func(t *testing.T) {
		if err := run(regular); err != nil {
			t.Fatalf("expected regular file to pass validation, got: %v", err)
		}
	})

	t.Run("symlink to regular file", func(t *testing.T) {
		link := filepath.Join(dir, "input-link")
		if err := os.Symlink(regular, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := run(link); err != nil {
			t.Fatalf("expected symlink to regular file to pass validation, got: %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		missing := filepath.Join(dir, "missing.txt")
		err := run(missing)
		if err == nil {
			t.Fatal("expected missing file to fail validation")
		}
		if !IsUserInputError(err) {
			t.Fatalf("expected UserInputError, got %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), "input") || !strings.Contains(err.Error(), missing) {
			t.Errorf("expected error to identify the parameter and path, got: %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		err := run(dir)
		if err == nil {
			t.Fatal("expected directory to fail validation")
		}
		if !IsUserInputError(err) {
			t.Fatalf("expected UserInputError, got %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), "input") || !strings.Contains(err.Error(), "regular file") {
			t.Errorf("expected error to identify the parameter and regular-file requirement, got: %v", err)
		}
	})
}

func TestValidationTag_File_OptionalPointerAbsent(t *testing.T) {
	type Params struct {
		Input *string `file:"true"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE(nil)
	if err != nil {
		t.Fatalf("expected absent optional file to skip validation, got: %v", err)
	}
}

func TestValidationTag_File_NonStringRejected(t *testing.T) {
	type Params struct {
		Input int `file:"true"`
	}

	_, err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).ToCobraE()
	if err == nil {
		t.Fatal("expected file tag on non-string field to fail command construction")
	}
	if !strings.Contains(err.Error(), "file tag requires a string field") {
		t.Errorf("expected string-field error, got: %v", err)
	}
}

func TestValidationTag_File_FalseDisabled(t *testing.T) {
	type Params struct {
		Input string `file:"false"`
	}

	err := (Cmd[Params]{
		Use:         "test",
		ParamEnrich: ParamEnricherName,
		RunFunc:     func(p *Params, cmd *cobra.Command, args []string) {},
	}).RunArgsE([]string{"--input", t.TempDir()})
	if err != nil {
		t.Fatalf("expected file:false to disable validation, got: %v", err)
	}
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

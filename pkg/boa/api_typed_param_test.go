package boa

import (
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

type FieldTestConfig struct {
	Name string `descr:"User name" optional:"true"`
	Port int    `descr:"Port number" default:"8080"`
}

func TestParam_SetDefault(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetDefault("default-name")
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "default-name" {
				t.Errorf("Expected Name to be 'default-name', got '%s'", params.Name)
			}
		},
		RawArgs: []string{},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetDefault_OverriddenByCLI(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetDefault("default-name")
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "cli-name" {
				t.Errorf("Expected Name to be 'cli-name', got '%s'", params.Name)
			}
		},
		RawArgs: []string{"--name", "cli-name"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetCustomValidator_Valid(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			portParam := Param(ctx, &params.Port)
			portParam.SetCustomValidator(func(port int) error {
				if port < 1 || port > 65535 {
					return fmt.Errorf("port must be between 1 and 65535")
				}
				return nil
			})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Port != 443 {
				t.Errorf("Expected Port to be 443, got %d", params.Port)
			}
		},
		RawArgs: []string{"--port", "443"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetCustomValidator_Invalid(t *testing.T) {
	config := FieldTestConfig{}

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			portParam := Param(ctx, &params.Port)
			portParam.SetCustomValidator(func(port int) error {
				if port < 1 || port > 65535 {
					return fmt.Errorf("port must be between 1 and 65535")
				}
				return nil
			})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			t.Error("RunFunc should not have been called")
		},
		RawArgs: []string{"--port", "99999"},
	}.RunE()

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}
}

func TestParam_SetCustomValidator_String(t *testing.T) {
	config := FieldTestConfig{}

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetDefault("x") // too short
			nameParam.SetCustomValidator(func(name string) error {
				if len(name) < 3 {
					return fmt.Errorf("name must be at least 3 characters")
				}
				return nil
			})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			t.Error("RunFunc should not have been called")
		},
		RawArgs: []string{},
	}.RunE()

	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}
}

func TestField_EmbedsParameter(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			underlying := nameParam.Parameter
			if underlying == nil {
				t.Error("Expected underlying Parameter, got nil")
			}
			return nil
		},
		PostCreateFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			if nameParam.GetName() != "name" {
				t.Errorf("Expected name 'name', got '%s'", nameParam.GetName())
			}
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
		},
		RawArgs: []string{"--name", "test"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_CombinedValidatorAndDefault(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetDefault("validname")
			nameParam.SetCustomValidator(func(name string) error {
				if len(name) < 3 {
					return fmt.Errorf("name must be at least 3 characters")
				}
				return nil
			})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "validname" {
				t.Errorf("Expected Name to be 'validname', got '%s'", params.Name)
			}
		},
		RawArgs: []string{},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetAlternatives_Valid(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetAlternatives([]string{"alice", "bob", "charlie"})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "bob" {
				t.Errorf("Expected Name to be 'bob', got '%s'", params.Name)
			}
		},
		RawArgs: []string{"--name", "bob"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetAlternatives_Invalid(t *testing.T) {
	config := FieldTestConfig{}

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetAlternatives([]string{"alice", "bob", "charlie"})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			t.Error("RunFunc should not have been called")
		},
		RawArgs: []string{"--name", "invalid"},
	}.RunE()

	if err == nil {
		t.Fatal("Expected validation error for invalid alternative, got nil")
	}
}

func TestParam_SetStrictAlts_False(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetAlternatives([]string{"alice", "bob", "charlie"})
			nameParam.SetStrictAlts(false)
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "custom" {
				t.Errorf("Expected Name to be 'custom', got '%s'", params.Name)
			}
		},
		RawArgs: []string{"--name", "custom"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetRequiredFn(t *testing.T) {
	type ConditionalConfig struct {
		Mode     string `descr:"Mode" default:"simple"`
		Advanced string `descr:"Advanced option" optional:"true"`
	}

	config := ConditionalConfig{}

	err := Cmd[ConditionalConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *ConditionalConfig, cmd *cobra.Command) error {
			advParam := Param(ctx, &params.Advanced)
			advParam.SetRequiredFn(func() bool {
				return params.Mode == "advanced"
			})
			return nil
		},
		RunFunc: func(params *ConditionalConfig, cmd *cobra.Command, args []string) {
			t.Error("RunFunc should not have been called")
		},
		RawArgs: []string{"--mode", "advanced"},
	}.RunE()

	if err == nil {
		t.Fatal("Expected error for missing conditionally required param")
	}
}

func TestParam_SetRequiredFn_NotRequired(t *testing.T) {
	type ConditionalConfig struct {
		Mode     string `descr:"Mode" default:"simple"`
		Advanced string `descr:"Advanced option" optional:"true"`
	}

	config := ConditionalConfig{}
	ran := false

	err := Cmd[ConditionalConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *ConditionalConfig, cmd *cobra.Command) error {
			advParam := Param(ctx, &params.Advanced)
			advParam.SetRequiredFn(func() bool {
				return params.Mode == "advanced"
			})
			return nil
		},
		RunFunc: func(params *ConditionalConfig, cmd *cobra.Command, args []string) {
			ran = true
		},
		RawArgs: []string{"--mode", "simple"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetIsEnabledFn(t *testing.T) {
	type FeatureConfig struct {
		Feature bool   `descr:"Enable feature" default:"false"`
		Setting string `descr:"Feature setting" optional:"true"`
	}

	config := FeatureConfig{}
	ran := false

	err := Cmd[FeatureConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FeatureConfig, cmd *cobra.Command) error {
			settingParam := Param(ctx, &params.Setting)
			settingParam.SetIsEnabledFn(func() bool {
				return params.Feature
			})
			return nil
		},
		RunFunc: func(params *FeatureConfig, cmd *cobra.Command, args []string) {
			ran = true
		},
		RawArgs: []string{},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetName(t *testing.T) {
	type SingleFieldConfig struct {
		Name string `descr:"User name" optional:"true"`
	}
	config := SingleFieldConfig{}
	ran := false

	err := Cmd[SingleFieldConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherNone,
		InitFuncCtx: func(ctx *HookContext, params *SingleFieldConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetName("custom-name")
			return nil
		},
		RunFunc: func(params *SingleFieldConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "test-value" {
				t.Errorf("Expected Name to be 'test-value', got '%s'", params.Name)
			}
		},
		RawArgs: []string{"--custom-name", "test-value"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetShort(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetShort("x")
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "short-value" {
				t.Errorf("Expected Name to be 'short-value', got '%s'", params.Name)
			}
		},
		RawArgs: []string{"-x", "short-value"},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetEnv(t *testing.T) {
	config := FieldTestConfig{}
	ran := false

	t.Setenv("CUSTOM_NAME_VAR", "env-value")

	err := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetEnv("CUSTOM_NAME_VAR")
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {
			ran = true
			if params.Name != "env-value" {
				t.Errorf("Expected Name to be 'env-value', got '%s'", params.Name)
			}
		},
		RawArgs: []string{},
	}.RunE()

	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if !ran {
		t.Fatal("RunFunc was not called")
	}
}

func TestParam_SetAlternativesFunc(t *testing.T) {
	config := FieldTestConfig{}

	cmd := Cmd[FieldTestConfig]{
		Use:         "test",
		Params:      &config,
		ParamEnrich: ParamEnricherName,
		InitFuncCtx: func(ctx *HookContext, params *FieldTestConfig, cmd *cobra.Command) error {
			nameParam := Param(ctx, &params.Name)
			nameParam.SetAlternativesFunc(func(cmd *cobra.Command, args []string, toComplete string) []string {
				return []string{"suggestion1", "suggestion2"}
			})
			return nil
		},
		RunFunc: func(params *FieldTestConfig, cmd *cobra.Command, args []string) {},
	}.ToCobra()

	flag := cmd.Flags().Lookup("name")
	if flag == nil {
		t.Fatal("Expected 'name' flag to exist")
	}
}

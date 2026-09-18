package boa

import (
	"reflect"

	"github.com/spf13/cobra"
)

// NoParams is an empty struct that can be used when a command doesn't need parameters.
type NoParams struct{}

// Cmd defines a command whose flags and arguments are derived from Struct.
// Struct must be a struct type. The zero value is ready to configure with a
// struct literal:
//
//	boa.Cmd[Params]{
//	    Use:   "my-app",
//	    Short: "description",
//	    RunFunc: func(params *Params, cmd *cobra.Command, args []string) {
//	        // use params directly
//	    },
//	}.Run()
type Cmd[Struct any] struct {
	// Use is the one-line usage message shown in help.
	Use string
	// Short is the short description shown in help.
	Short string
	// Long is the detailed description shown by help for this command.
	Long string
	// Version is the version reported by this command.
	Version string
	// Aliases are alternative names for this command.
	Aliases []string
	// GroupID assigns this command to a Cobra help group.
	GroupID string
	// Groups defines Cobra help groups for subcommands.
	Groups []*cobra.Group
	// Args validates positional arguments.
	Args cobra.PositionalArgs
	// SubCmds contains this command's children.
	SubCmds []*cobra.Command
	// ParamEnrich customizes parameter metadata before flags are bound.
	ParamEnrich ParamEnricher
	// UseCobraErrLog enables Cobra's error output.
	UseCobraErrLog bool
	// SortFlags sorts flags alphabetically in help output.
	SortFlags bool
	// ValidArgs lists accepted non-flag arguments for completion.
	ValidArgs []string
	// ConfigFormat overrides extension-based config-format selection for this command.
	ConfigFormat ConfigFormat
	// RawArgs supplies arguments instead of os.Args.
	RawArgs []string
	// Params is a pointer to the struct containing command parameters
	Params *Struct
	// RunFunc is the function to run when this command is called, with type-safe parameters
	RunFunc func(params *Struct, cmd *cobra.Command, args []string)
	// RunFuncCtx is the function to run when this command is called, with access to HookContext
	RunFuncCtx func(ctx *HookContext, params *Struct, cmd *cobra.Command, args []string)
	// RunFuncE is like RunFunc but returns an error
	RunFuncE func(params *Struct, cmd *cobra.Command, args []string) error
	// RunFuncCtxE is like RunFuncCtx but returns an error
	RunFuncCtxE func(ctx *HookContext, params *Struct, cmd *cobra.Command, args []string) error
	// InitFunc runs during initialization with type-safe parameters
	InitFunc func(params *Struct, cmd *cobra.Command) error
	// PostCreateFunc runs after cobra flags are created but before parsing
	PostCreateFunc func(params *Struct, cmd *cobra.Command) error
	// PreValidateFunc runs after flags are parsed but before validation
	PreValidateFunc func(params *Struct, cmd *cobra.Command, args []string) error
	// PreExecuteFunc runs after validation but before command execution
	PreExecuteFunc func(params *Struct, cmd *cobra.Command, args []string) error
	// InitFuncCtx runs during initialization with access to HookContext
	InitFuncCtx func(ctx *HookContext, params *Struct, cmd *cobra.Command) error
	// PostCreateFuncCtx runs after cobra flags are created with access to HookContext
	PostCreateFuncCtx func(ctx *HookContext, params *Struct, cmd *cobra.Command) error
	// PreValidateFuncCtx runs after flags are parsed but before validation with HookContext
	PreValidateFuncCtx func(ctx *HookContext, params *Struct, cmd *cobra.Command, args []string) error
	// PreExecuteFuncCtx runs after validation but before execution with HookContext
	PreExecuteFuncCtx func(ctx *HookContext, params *Struct, cmd *cobra.Command, args []string) error
	// ValidArgsFunc is a function returning valid arguments for bash completion
	ValidArgsFunc func(params *Struct, cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
}

func (b Cmd[Struct]) command() command {

	if b.Params == nil {
		b.Params = new(Struct)
	}

	if reflect.TypeFor[Struct]().Kind() != reflect.Struct {
		panic("expected pointer to struct")
	}

	command := command{
		Use:            b.Use,
		Short:          b.Short,
		Long:           b.Long,
		Version:        b.Version,
		Aliases:        b.Aliases,
		GroupID:        b.GroupID,
		Groups:         b.Groups,
		Args:           b.Args,
		SubCmds:        b.SubCmds,
		ParamEnrich:    b.ParamEnrich,
		UseCobraErrLog: b.UseCobraErrLog,
		SortFlags:      b.SortFlags,
		ValidArgs:      b.ValidArgs,
		ConfigFormat:   b.ConfigFormat,
		RawArgs:        b.RawArgs,
		Params:         b.Params,

		InitFunc:           adaptSetup(b.InitFunc),
		PostCreateFunc:     adaptSetup(b.PostCreateFunc),
		PreValidateFunc:    adaptPhase(b.PreValidateFunc),
		PreExecuteFunc:     adaptPhase(b.PreExecuteFunc),
		InitFuncCtx:        adaptSetupCtx(b.InitFuncCtx),
		PostCreateFuncCtx:  adaptSetupCtx(b.PostCreateFuncCtx),
		PreValidateFuncCtx: adaptPhaseCtx(b.PreValidateFuncCtx),
		PreExecuteFuncCtx:  adaptPhaseCtx(b.PreExecuteFuncCtx),

		newParams: func() any { return new(Struct) },
	}
	if b.RunFunc != nil {
		command.RunFunc = func(cmd *cobra.Command, args []string) { b.RunFunc(b.Params, cmd, args) }
	}
	if b.RunFuncE != nil {
		command.RunFuncE = func(cmd *cobra.Command, args []string) error { return b.RunFuncE(b.Params, cmd, args) }
	}
	if b.RunFuncCtx != nil {
		command.RunFuncCtx = func(ctx *HookContext, cmd *cobra.Command, args []string) { b.RunFuncCtx(ctx, b.Params, cmd, args) }
	}
	if b.RunFuncCtxE != nil {
		command.RunFuncCtxE = func(ctx *HookContext, cmd *cobra.Command, args []string) error {
			return b.RunFuncCtxE(ctx, b.Params, cmd, args)
		}
	}
	if b.ValidArgsFunc != nil {
		command.ValidArgsFunc = func(cmd *cobra.Command, args []string, text string) ([]string, cobra.ShellCompDirective) {
			return b.ValidArgsFunc(b.Params, cmd, args, text)
		}
	}
	return command

}

// ToCobra converts this command to a cobra.Command.
func (b Cmd[Struct]) ToCobra() *cobra.Command {
	return b.command().ToCobra()
}

// Run executes the command with default error handling.
func (b Cmd[Struct]) Run() {
	runH(b.ToCobra(), resultHandler{})
}

// RunArgs executes the command with the provided arguments and default error handling.
func (b Cmd[Struct]) RunArgs(rawArgs []string) {
	b.RawArgs = rawArgs
	b.Run()
}

// Validate validates this command's parameters, skipping subcommands and all action hooks.
func (b Cmd[Struct]) Validate() error {
	return b.command().Validate()
}

// ToCobraE converts this command to a cobra.Command that uses RunE for error handling.
func (b Cmd[Struct]) ToCobraE() (*cobra.Command, error) {
	return b.command().ToCobraE()
}

// RunE executes the command and returns any error that occurred.
func (b Cmd[Struct]) RunE() error {
	return b.command().RunE()
}

// RunArgsE executes the command with the provided arguments and returns any error.
func (b Cmd[Struct]) RunArgsE(rawArgs []string) error {
	b.RawArgs = rawArgs
	return b.RunE()
}

func adaptSetup[T any](fn func(*T, *cobra.Command) error) func(any, *cobra.Command) error {
	if fn == nil {
		return nil
	}
	return func(params any, cmd *cobra.Command) error { return fn(params.(*T), cmd) }
}
func adaptSetupCtx[T any](fn func(*HookContext, *T, *cobra.Command) error) func(*HookContext, any, *cobra.Command) error {
	if fn == nil {
		return nil
	}
	return func(ctx *HookContext, params any, cmd *cobra.Command) error { return fn(ctx, params.(*T), cmd) }
}
func adaptPhase[T any](fn func(*T, *cobra.Command, []string) error) func(any, *cobra.Command, []string) error {
	if fn == nil {
		return nil
	}
	return func(params any, cmd *cobra.Command, args []string) error { return fn(params.(*T), cmd, args) }
}
func adaptPhaseCtx[T any](fn func(*HookContext, *T, *cobra.Command, []string) error) func(*HookContext, any, *cobra.Command, []string) error {
	if fn == nil {
		return nil
	}
	return func(ctx *HookContext, params any, cmd *cobra.Command, args []string) error {
		return fn(ctx, params.(*T), cmd, args)
	}
}

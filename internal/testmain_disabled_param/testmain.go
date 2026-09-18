package main

import (
	"fmt"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type disabledParamParams struct {
	Foo string `optional:"true"`
	Bar int    `optional:"true"`
	Baz string `optional:"true"`
}

func main() {

	var params disabledParamParams

	if err := (boa.Cmd[disabledParamParams]{
		Use:   "hello-world",
		Short: "a generic cli tool",
		Long:  `A generic cli tool that has a longer description. See the README.MD for more information`,
		ParamEnrich: boa.ParamEnricherCombine(
			boa.ParamEnricherName,
			boa.ParamEnricherShort,
		),
		Params: &params,
		InitFuncCtx: func(ctx *boa.HookContext, p *disabledParamParams, cmd *cobra.Command) error {
			boa.Param(ctx, &p.Bar).SetIsEnabledFn(func() bool {
				return ctx.HasValue(&p.Foo)
			})
			boa.Param(ctx, &p.Baz).SetRequiredFn(func() bool {
				return ctx.HasValue(&p.Foo)
			})
			return nil
		},
		RunFunc: func(p *disabledParamParams, cmd *cobra.Command, args []string) {
			fmt.Printf("Hello World!\n")
		},
	}.RunE()); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

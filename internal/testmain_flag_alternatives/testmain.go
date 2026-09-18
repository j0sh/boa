package main

import (
	"fmt"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

func main() {
	type subParams struct {
		Foo string `alts:"abc,cde,fgh"`
	}
	var subCommand1Params subParams

	boa.Cmd[boa.NoParams]{
		Use:     "hello-world",
		Short:   "a generic cli tool",
		Long:    `A generic cli tool that has a longer description.See the README.MD for more information`,
		Version: "v1.2.3",
		SubCmds: []*cobra.Command{
			boa.Cmd[subParams]{
				Use:         "subcommand1",
				Short:       "a subcommand",
				Params:      &subCommand1Params,
				ParamEnrich: boa.ParamEnricherCombine(boa.ParamEnricherName, boa.ParamEnricherEnv),
				RunFunc: func(_ *subParams, cmd *cobra.Command, args []string) {
					p1 := subCommand1Params.Foo
					fmt.Printf("Hello world from subcommand1 with params: %s\n", p1)
				},
			}.ToCobra(),
			boa.Cmd[boa.NoParams]{
				Use:   "subcommand2",
				Short: "a subcommand",
				RunFunc: func(_ *boa.NoParams, cmd *cobra.Command, args []string) {
					fmt.Println("Hello world from subcommand2")
				},
			}.ToCobra(),
		},
	}.Run()
}

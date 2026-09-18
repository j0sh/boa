package main

import (
	"fmt"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

func main() {
	type paramsT struct {
		WithoutDefaults []float64
		WithDefaults    []int64 `default:"[1,2,3]"`
	}
	var params paramsT

	if err := (boa.Cmd[paramsT]{
		Use:    "hello-world",
		Short:  "a generic cli tool",
		Long:   `A generic cli tool that has a longer description. See the README.MD for more information`,
		Params: &params,
		RunFunc: func(_ *paramsT, cmd *cobra.Command, args []string) {
			fmt.Printf(
				"params: without=%v, with=%v\n",
				params.WithoutDefaults,
				params.WithDefaults,
			)
		},
	}.RunE()); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// Example matching the README and Getting Started quick start.
package main

import (
	"fmt"

	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type Params struct {
	Name    string `descr:"name to greet"`
	Count   int    `descr:"number of greetings" default:"1"`
	Excited bool   `descr:"add an exclamation mark" optional:"true"`
}

func main() {
	boa.Cmd[Params]{
		Use:   "greet",
		Short: "print a greeting",
		RunFunc: func(p *Params, _ *cobra.Command, _ []string) {
			suffix := ""
			if p.Excited {
				suffix = "!"
			}
			for range p.Count {
				fmt.Printf("Hello %s%s\n", p.Name, suffix)
			}
		},
	}.Run()
}

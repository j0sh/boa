package main

import (
	"fmt"

	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

func main() {
	boa.Cmd[boa.NoParams]{
		Use:   "myapp",
		Short: "An app demonstrating aliases and groups",
		Long:  "An example CLI app that demonstrates command aliases and help grouping",
		// Define a custom group title for "server", let "tools" be auto-generated
		Groups: []*cobra.Group{
			{ID: "server", Title: "Server Commands:"},
		},
		SubCmds: boa.SubCmds(
			// Server commands with aliases
			boa.Cmd[boa.NoParams]{
				Use:     "start",
				Short:   "Start the server",
				Aliases: []string{"up", "run"},
				GroupID: "server",
				RunFunc: func(_ *boa.NoParams, cmd *cobra.Command, args []string) {
					fmt.Println("Server started!")
				},
			},
			boa.Cmd[boa.NoParams]{
				Use:     "stop",
				Short:   "Stop the server",
				Aliases: []string{"down"},
				GroupID: "server",
				RunFunc: func(_ *boa.NoParams, cmd *cobra.Command, args []string) {
					fmt.Println("Server stopped!")
				},
			},
			// Tool commands - group will be auto-generated as "tools:"
			boa.Cmd[boa.NoParams]{
				Use:     "lint",
				Short:   "Run linter",
				GroupID: "tools",
				RunFunc: func(_ *boa.NoParams, cmd *cobra.Command, args []string) {
					fmt.Println("Linting...")
				},
			},
			boa.Cmd[boa.NoParams]{
				Use:     "format",
				Short:   "Format code",
				Aliases: []string{"fmt"},
				GroupID: "tools",
				RunFunc: func(_ *boa.NoParams, cmd *cobra.Command, args []string) {
					fmt.Println("Formatting...")
				},
			},
		),
	}.Run()
}

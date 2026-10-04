// Example of a base directory for config and input files.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type Params struct {
	DataDir    string `basedir:"required,autocreate" default:"." env:"APP_DATA_DIR" descr:"base directory for relative file paths"`
	ConfigFile string `configfile:"true" optional:"true" default:"config.json"`
	Input      string `file:"true" optional:"true"`
}

func command(out io.Writer) boa.Cmd[Params] {
	return boa.Cmd[Params]{
		Use: "basedir-example",
		RunFuncE: func(p *Params, _ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(out, "Data directory: %s\nConfig: %s\nInput: %s\n", p.DataDir, p.ConfigFile, p.Input)
			return err
		},
	}
}

func main() { command(os.Stdout).Run() }

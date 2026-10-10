// Example of a base directory for config and input files.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type Params struct {
	DataDir    string `basedir:"required,autocreate" default:"." env:"APP_DATA_DIR" descr:"base directory for relative file paths"`
	Network    string `default:"mainnet" env:"APP_NETWORK" descr:"network to run on (mainnet or testnet)"`
	ConfigFile string `configfile:"optional-default" default:"config.json"`
	Input      string `file:"true" optional:"true"`
	RunnerFile string `file:"true" basepath:"source" optional:"true"`
	StateFile  string `basepath:"basedir" default:"state.db"`
}

func command(out io.Writer) boa.Cmd[Params] {
	return boa.Cmd[Params]{
		Use: "basedir-example",
		PostConfigFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command, _ []string) error {
			// Check the network before BOA creates the final base directory.
			switch p.Network {
			case "mainnet", "testnet":
			default:
				return boa.NewUserInputErrorf("unsupported network %q: choose mainnet or testnet", p.Network)
			}
			// Isolate network state using the merged config, preserving explicit paths.
			if !ctx.HasInput(&p.DataDir) {
				p.DataDir = filepath.Join("data", p.Network)
			}
			return nil
		},
		RunFuncE: func(p *Params, _ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(out, "Network: %s\nData directory: %s\nConfig: %s\nInput: %s\nRunner: %s\nState: %s\n", p.Network, p.DataDir, p.ConfigFile, p.Input, p.RunnerFile, p.StateFile)
			return err
		},
	}
}

func main() { command(os.Stdout).Run() }

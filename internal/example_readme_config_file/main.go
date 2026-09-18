// Example matching the README's Config Files section.
package main

import (
	"fmt"
	"github.com/j0sh/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type ConfigFromFile struct {
	ConfigFile string     `configfile:"true" optional:"true" default:"config.json"`
	Host       string     `descr:"server host" env:"HOST"`
	Port       int        `descr:"port" default:"8080"`
	Internal   [][]string `boa:"configonly"`
}

func main() {
	boa.Cmd[ConfigFromFile]{Use: "my-app", RunFunc: func(p *ConfigFromFile, _ *cobra.Command, _ []string) {
		fmt.Printf("Host: %s, Port: %d\n", p.Host, p.Port)
	}}.Run()
}

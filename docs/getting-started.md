# Getting Started

## Install

```bash
go get github.com/j0sh/boa@latest
```

BOA requires Go 1.27 or later.

## Build a command

This is a complete program:

```go
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
```

BOA derives `--name`, `--count`, and `--excited`, assigns non-conflicting short flags, parses values, validates required fields, and generates Cobra help.

```text
$ greet --name Ada --count 2 --excited
Hello Ada!
Hello Ada!
```

The complete tested version is in [`internal/example_readme_minimum`](https://github.com/j0sh/boa/tree/main/internal/example_readme_minimum).

## Required and optional fields

Plain scalar and flat-slice fields are required by default. A default satisfies the requirement. Use `optional:"true"` for an ordinary zero-valued optional field, or use a pointer when absence matters:

```go
type Params struct {
    Host    string `default:"localhost"` // has a default
    Debug   bool   `optional:"true"`     // false when absent
    Retries *int                         // nil when absent, &0 for --retries 0
}
```

Maps, nested slices, and pointer fields default to optional. `required:"true"` overrides that default. Applications that want all plain fields optional can call `boa.Init(boa.WithDefaultOptional())` before constructing commands.

## Common field shapes

| Go field | CLI form | Notes |
|---|---|---|
| `string`, numeric, `bool` | `--name value` | Native Cobra/pflag parsing |
| `time.Duration`, `time.Time`, `net.IP`, `*url.URL` | textual value | Built-in parsers |
| `[]string`, numeric slices | comma-separated | Repeatable; `collection:"array"` changes occurrence semantics |
| `map[string]string`, `map[string]int` | `key=value,key=value` | Maps default to optional |
| nested slices and complex maps | JSON | For example `--matrix '[[1,2],[3,4]]'` |
| `positional:"true"` field | bare argument | Positional fields must be declared in order |

See [Parameters and Struct Tags](struct-tags.md) for the complete behavior and tag table.

## Positional arguments

```go
type CopyParams struct {
    Source string `positional:"true" descr:"source path"`
    Dest   string `positional:"true" descr:"destination path"`
}
```

Required positional fields must precede optional ones. A final slice positional consumes the remaining arguments.

## Struct composition

Embedded fields stay flat. Named fields prefix their children:

```go
type Network struct {
    Host string `default:"localhost"`
    Port int    `default:"8080"`
}

type Common struct {
    Verbose bool `optional:"true"`
}

type Params struct {
    Common          // --verbose
    API     Network // --api-host, --api-port
    Admin   Network // --admin-host, --admin-port
}
```

Prefixes also apply to environment names and explicit `name`/`env` tags. Deep nesting chains prefixes.

## Subcommands

Use `boa.NoParams` for a command with no fields and `boa.SubCmds` to combine typed children:

```go
root := boa.Cmd[boa.NoParams]{
    Use: "tool",
    SubCmds: boa.SubCmds(
        boa.Cmd[ServeParams]{Use: "serve", RunFunc: runServe},
        boa.Cmd[DeployParams]{Use: "deploy", RunFunc: runDeploy},
    ),
}
root.Run()
```

`SubCmds` returns ordinary `[]*cobra.Command`, so BOA and native Cobra commands can be mixed. See [Cobra Interoperability](cobra-interop.md).

## Environment variables and config files

Environment variables are opt-in through the `env` tag or `ParamEnricherEnv`. Config files are opt-in through a `configfile:"true"` string or string-slice field:

```go
type Params struct {
    ConfigFile string `configfile:"true" optional:"true" default:"config.json"`
    Host       string `env:"HOST" default:"localhost"`
    Port       int    `env:"PORT" default:"8080"`
}
```

When several sources set the same field, BOA uses this order:

1. CLI flags and positional arguments
2. environment variables
3. root config files
4. nested-struct config files
5. defaults
6. Go zero values

See [Configuration Files](configuration.md) for formats, overlay chains, discovery, explicit loading, and dumping.

## Errors and testing

`Run()` supplies command-line application behavior. `RunE()` and `RunArgsE()` return errors and are usually better for tests or embedding:

```go
err := boa.Cmd[Params]{Use: "greet"}.RunArgsE([]string{"--name", "Ada"})
```

See [Lifecycle and Errors](lifecycle.md) for hook order, error categories, validation-only execution, and test patterns.

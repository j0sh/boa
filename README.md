# BOA

[![CI Status](https://github.com/j0sh/boa/actions/workflows/ci.yml/badge.svg)](https://github.com/j0sh/boa/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/j0sh/boa)](https://goreportcard.com/report/github.com/j0sh/boa)
[![Docs](https://img.shields.io/badge/docs-j0sh.github.io%2Fboa-blue)](https://j0sh.github.io/boa/)

BOA is a declarative Go CLI framework built on [Cobra](https://github.com/spf13/cobra). Define a parameter struct once and BOA derives flags, positional arguments, environment bindings, validation, config-file loading, and help text.

This fork is based on [GiGurra/boa](https://github.com/GiGurra/boa). See the [migration guide](https://j0sh.github.io/boa/migration/) for the API differences.

## Install

```bash
go get github.com/j0sh/boa@latest
```

BOA requires Go 1.27 or later.

## Quick start

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

```text
$ greet --name Ada --count 2 --excited
Hello Ada!
Hello Ada!

$ greet --help
Flags:
  -n, --name string   name to greet (required)
  -c, --count int     number of greetings (default 1)
  -e, --excited       add an exclamation mark
  -h, --help          help for greet
```

Plain fields are required unless they have a default or are marked optional. Pointer fields are optional and preserve the difference between “not supplied” and an explicit zero value.

```go
type Params struct {
    Retries *int              // nil, or the exact value supplied
    Labels  map[string]string // --labels env=prod,team=backend
    Files   []string          `positional:"true"`
}
```

Values resolve in the following order:

1. CLI
2. environment
3. root config file
4. nested config file
5. default
6. Go zero value

## Features

- Struct-derived flags, positional arguments, defaults, and help
- Required, enum, range, length, pattern, and custom validation
- Built-in JSON config support with pluggable TOML, YAML or custom formats
- Nested structs with namespacing
- Lifecycle hooks and type-safe programmatic field configuration
- Cobra command trees, completion, groups, and ecosystem interoperability
- Live reload API

## Documentation

- [Getting started](https://j0sh.github.io/boa/getting-started/)
- [Parameters and struct tags](https://j0sh.github.io/boa/struct-tags/)
- [Configuration files](https://j0sh.github.io/boa/configuration/)
- [Lifecycle and errors](https://j0sh.github.io/boa/lifecycle/)
- [Cobra interoperability](https://j0sh.github.io/boa/cobra-interop/)
- [Recipes and runnable examples](https://j0sh.github.io/boa/recipes/)
- [Migrating from GiGurra/boa](https://j0sh.github.io/boa/migration/)

API documentation is available on [pkg.go.dev](https://pkg.go.dev/github.com/j0sh/boa/pkg/boa).

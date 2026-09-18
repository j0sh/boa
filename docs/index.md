# BOA

**Declarative Go CLIs built on Cobra.**

BOA turns a Go struct into a Cobra command while leaving Cobra's command tree and extension points available. Fields can be populated from flags, positional arguments, environment variables, config files, defaults, or application code.

```go
type Params struct {
    Host string        `env:"HOST" default:"localhost"`
    Port int           `short:"p" min:"1" max:"65535"`
    Wait time.Duration `default:"30s"`
}

boa.Cmd[Params]{
    Use: "serve",
    RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
        serve(p.Host, p.Port, p.Wait)
    },
}.Run()
```

## Choose a path

- **New to BOA?** Start with [Getting Started](getting-started.md).
- **Defining fields?** Use [Parameters and Struct Tags](struct-tags.md) as the canonical reference for types, tags, validation, enrichment, and source precedence.
- **Loading configuration?** See [Configuration Files](configuration.md), then [Config Decoding](config-decoding.md) for custom scalar and decoder contracts.
- **Integrating an existing struct?** See [External Structs](external-structs.md).
- **Embedding BOA in a Cobra application?** See [Cobra Interoperability](cobra-interop.md).
- **Building a long-running service?** See [Live Config Reload](live-reload.md).
- **Coming from the original project?** See [Migrating from GiGurra/boa](migration.md).

## Core contracts

- Source precedence is CLI > env > root config > nested config > default > zero value.
- Metadata changed programmatically must be set in `InitFunc` or `InitFuncCtx`, before flags and environment values are bound.
- Pointer substructures are allocated for traversal, then restored to `nil` unless an input source populated them. Defaults alone do not keep them alive.
- `boa:"ignore"` removes a field from BOA processing. `boa:"configonly"` keeps validation while disabling CLI and environment input.
- Config formats normally dispatch by file extension. `Cmd.ConfigFormat` deliberately bypasses that registry for one command.

## Install

```bash
go get github.com/j0sh/boa@latest
```

BOA requires Go 1.27 or later. The public package reference is on [pkg.go.dev](https://pkg.go.dev/github.com/j0sh/boa/pkg/boa).

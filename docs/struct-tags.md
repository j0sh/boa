# Parameters and Struct Tags

This page is the canonical reference for BOA fields: supported shapes, struct tags, validation, source precedence, enrichment, and programmatic configuration.

## Source precedence

When more than one source sets a field, the highest source wins:

1. CLI flags and positional arguments
2. environment variables
3. root config files
4. nested-struct config files
5. defaults
6. Go zero values

BOA tracks whether a source supplied a value separately from the value itself. An explicit `0`, `false`, empty string, or config value equal to the default is still present.

## Tag reference

| Tag | Meaning | Example |
|---|---|---|
| `descr` | Help description | `descr:"server port"` |
| `name` | Override the long flag name | `name:"listen-port"` |
| `short` | Set a one-character shorthand | `short:"p"` |
| `env` | Bind an environment variable | `env:"PORT"` |
| `default` | Parse and install a default | `default:"8080"` |
| `required` | Explicitly require or unrequire | `required:"true"` |
| `optional` | Explicitly make optional or required | `optional:"true"` |
| `positional` | Consume a positional argument | `positional:"true"` |
| `persistent` | Inherit the flag in child commands | `persistent:"true"` |
| `alts` | Comma-separated completion/validation values | `alts:"debug,info,warn"` |
| `strict` | Enforce alternatives; defaults to true | `strict:"false"` |
| `min`, `max` | Numeric bound or collection/string length | `min:"1" max:"65535"` |
| `pattern` | Regular expression for a string | `pattern:"^[a-z][a-z0-9-]*$"` |
| `file` | Require an existing regular file | `file:"true"` |
| `collection` | Slice occurrence mode: `slice` or `array` | `collection:"array"` |
| `configfile` | Load path(s) into the enclosing struct | `configfile:"true"` |
| `boa` | Processing directives | `boa:"configonly"` |

Tag values are applied before flags are bound and environment variables are read. Invalid bounds, defaults, or tag combinations fail command construction.

## Required and optional

Plain scalar and flat-slice fields are required by default. These field shapes default to optional:

- pointers, because `nil` represents absence;
- maps;
- nested slices such as `[][]int`.

`required:"true"` and `optional:"true"` override the default. A declared default also satisfies a required field.

```go
type Params struct {
    Host    string `default:"localhost"`
    Port    int
    Debug   bool `optional:"true"`
    Retries *int
    Labels  map[string]string
}
```

To make plain fields optional throughout an application, call this before creating commands:

```go
boa.Init(boa.WithDefaultOptional())
```

Explicit `required` and `optional` tags still win over the global setting.

## Flags and positional arguments

BOA normally derives a kebab-case flag name from the field name and attempts to assign the first non-conflicting character as a shorthand. `HTTPPort` becomes `--http-port`; `-h` remains reserved for help.

Use `positional:"true"` for arguments without flag names:

```go
type Params struct {
    Input  string   `positional:"true"`
    Output string   `positional:"true" optional:"true" default:"stdout"`
    Extra  []string `positional:"true" optional:"true"`
}
```

Required positionals must precede optional positionals. A positional slice must be last. Positional fields cannot be combined with `boa:"noflag"` or `boa:"ignore"`.

`persistent:"true"` registers a flag on Cobra's persistent flag set so descendant commands inherit it. BOA suppresses auto-generated shorthand characters that would collide in the assembled subtree.

## Environment variables and enrichers

An `env` tag binds one field directly:

```go
type Params struct {
    Host string `env:"APP_HOST" default:"localhost"`
}
```

Environment names are not derived by default. To derive them for all fields, compose the environment enricher with the default chain:

```go
boa.Cmd[Params]{
    Use: "app",
    ParamEnrich: boa.ParamEnricherCombine(
        boa.ParamEnricherDefault,
        boa.ParamEnricherEnv,
        boa.ParamEnricherEnvPrefix("MYAPP"),
    ),
}
```

The built-in enrichers are:

| Enricher | Behavior |
|---|---|
| `ParamEnricherDefault` | Name + shorthand + Boolean false default |
| `ParamEnricherName` | Derive a kebab-case flag name |
| `ParamEnricherShort` | Assign a non-conflicting shorthand |
| `ParamEnricherEnv` | Derive `UPPER_SNAKE_CASE` from the flag name |
| `ParamEnricherEnvPrefix(prefix)` | Prefix an existing environment name |
| `ParamEnricherBool` | Give Boolean fields a false default |
| `ParamEnricherNone` | Disable enrichment |

Explicit tags take precedence over enrichers.

## Collections and complex values

Flat slices use Cobra's slice semantics by default, including comma splitting:

```go
type Params struct {
    Tags []string `default:"[red,blue]"`
}
// --tags red,blue
```

Use `collection:"array"` when each flag occurrence should be one scalar value without CSV splitting:

```go
type Params struct {
    Label []string `collection:"array"`
}
// --label 'one,opaque' --label two
```

Simple string-keyed maps accept `key=value` pairs:

```go
type Params struct {
    Labels map[string]string // --labels env=prod,team=platform
    Limits map[string]int    // --limits cpu=4,memory=8192
}
```

Nested slices, complex maps, and other values without a native flag handler use JSON:

```go
type Params struct {
    Matrix [][]int             `optional:"true"`
    Meta   map[string][]string `optional:"true"`
}
// --matrix '[[1,2],[3,4]]' --meta '{"owners":["ada"]}'
```

## Validation

`alts` supplies completion candidates and, by default, restricts the value:

```go
type Params struct {
    Level string `alts:"debug,info,warn,error"`
    Color string `alts:"red,green,blue" strict:"false"` // suggestions only
}
```

`min` and `max` constrain numeric values and the length of strings, slices, and maps. `pattern` applies a regular expression to strings:

```go
type Params struct {
    Port int      `min:"1" max:"65535"`
    Name string   `min:"3" max:"20" pattern:"^[a-z][a-z0-9-]*$"`
    Tags []string `min:"1" max:"5"`
    Input string  `file:"true"`
}
```

`file:"true"` accepts string paths that resolve to existing regular files. Symlinks are followed. Validation of an absent optional pointer is skipped; when present, its pointed-to value is validated normally.

For application logic, install typed validators in `InitFuncCtx`:

```go
InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    boa.Param(ctx, &p.Port).SetCustomValidator(func(port int) error {
        if port < 1024 && port != 80 && port != 443 {
            return fmt.Errorf("unsupported privileged port %d", port)
        }
        return nil
    })
    return nil
},
```

Conditional requirements and visibility use functions evaluated at runtime:

```go
field := boa.Param(ctx, &p.Token)
field.SetRequiredFn(func() bool { return p.Environment == "production" })
field.SetIsEnabledFn(func() bool { return p.AuthMode != "none" })
```

## BOA processing directives

The `boa` tag accepts comma-separated directives:

| Directive | CLI | Environment | Config decode | Validation/mirror |
|---|---:|---:|---:|---:|
| `noflag` | no | yes | yes | yes |
| `noenv` | yes | no | yes | yes |
| `noconfig` | yes | yes | explicit key rejected | yes |
| `configonly` | no | no | yes | yes |
| `ignore` | no | no | raw decoder only | no |

Use `configonly` for validated configuration that must not be exposed as a flag or environment variable. Use `ignore` for opaque data BOA should not traverse or validate.

Use `noconfig` when a field may come from flags, environment variables, defaults, or application code but must not appear in a config file. The check runs before decoding and rejects the whole file even if a higher-precedence source already supplied the field. Combine it with `noflag` for an environment-only secret:

```go
type Params struct {
    WebhookToken string `boa:"noflag,noconfig" env:"TOP_SECRET_WEBHOOK_TOKEN"`
}
```

On a struct group, `noconfig` also excludes its descendants, including embedded fields. Managed dumps omit excluded fields and groups; if nothing remains, they produce an empty object.

Custom formats must provide `ConfigFormat.KeyTree` so BOA can inspect literal keys. `RegisterConfigFormat` supplies one automatically for decoders that can also decode into `map[string]any`; a complete `ConfigFormat` without a key tree fails closed when an applicable `noconfig` field exists.

```go
type Params struct {
    ConfigFile string         `configfile:"true" optional:"true"`
    Secret     string         `boa:"noflag" env:"APP_SECRET" min:"20"`
    EnvOnly    string         `boa:"noflag,noconfig" env:"ENV_ONLY_SECRET"`
    InternalID string         `boa:"configonly" min:"8"`
    PluginData map[string]any `boa:"ignore"`
}
```

## Struct composition and prefixes

Anonymous embedded structs stay flat. Named structs prefix every descendant:

```go
type Server struct {
    Host string `env:"HOST" default:"localhost"`
    Port int    `name:"port" default:"8080"`
}

type Params struct {
    Primary Server // --primary-host, --primary-port; PRIMARY_HOST
    Replica Server // --replica-host, --replica-port; REPLICA_HOST
}
```

Explicit `name` and `env` tags are also prefixed. Deep nesting chains: `Infra.Primary.Host` becomes `--infra-primary-host`.

Optional struct pointers are temporarily allocated so BOA can discover their fields. After sourcing and validation, an untouched group returns to `nil`. A child default alone does not keep the pointer alive; a CLI, environment, or config value does.

## Programmatic field configuration

Use `boa.Param(ctx, &p.Field)` inside `InitFuncCtx` or an `InitCtx` struct method. It returns `*boa.Field[T]`, embedding the general `boa.Parameter` metadata API.

```go
InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    port := boa.Param(ctx, &p.Port)
    port.SetDefault(8080)
    port.SetMin(1)
    port.SetMax(65535)
    port.SetEnv("PORT")
    return nil
},
```

Typed methods catch the common type errors at compile time:

| Method | Applies to |
|---|---|
| `SetDefault(T)` | every field |
| `SetCustomValidator(func(T) error)` | every field |
| `SetMin(T)`, `SetMax(T)` | numeric fields |
| `SetMinLen(int)`, `SetMaxLen(int)` | strings, slices, maps |

The embedded `Parameter` also exposes name, shorthand, environment, alternatives, collection mode, required/enabled functions, processing directives, pattern, persistence, config-file status, and untyped min/max controls.

Programmatic metadata must be set during initialization. Later hooks run after binding and cannot retroactively add flags or environment bindings.

`ctx.HasValue(&p.Field)` reports whether any source supplied a value. `ctx.AllMirrors()` supports bulk inspection and custom enrichers.

## Custom scalar types

For types you own, implement `encoding.TextUnmarshaler` and preferably `encoding.TextMarshaler`. For types you cannot change, use `boa.RegisterType`. See [Config Decoding](config-decoding.md) for sharing the same textual behavior across flags, environment variables, JSON, YAML, and TOML.

# External Structs

BOA does not require tags. A generated, third-party, or shared struct can be used directly and configured through `InitFuncCtx`.

## Use the external struct as the command parameters

```go
boa.Cmd[httpserver.Config]{
    Use: "serve",
    InitFuncCtx: func(ctx *boa.HookContext, p *httpserver.Config, _ *cobra.Command) error {
        host := boa.Param(ctx, &p.Host)
        host.SetDefault("127.0.0.1")
        host.SetEnv("HOST")

        port := boa.Param(ctx, &p.Port)
        port.SetDefault(8080)
        port.SetMin(1)
        port.SetMax(65535)

        token := boa.Param(ctx, &p.AdminToken)
        token.SetNoFlag(true)
        token.SetEnv("ADMIN_TOKEN")
        token.SetRequired(false)
        return nil
    },
    RunFunc: runServer,
}.Run()
```

BOA derives field names normally. Programmatic metadata must be set during Init, before flags and environment variables are bound.

## Compose an external section

Named fields receive the normal namespace prefix:

```go
type Params struct {
    Environment string `alts:"dev,staging,production"`
    DB          dbconfig.Config
}
```

`DB.Host` becomes `--db-host`; a derived environment binding becomes `DB_HOST`. This avoids collisions when the same external type appears more than once.

Configure its fields through the live parameter tree:

```go
InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    boa.Param(ctx, &p.DB.Host).SetEnv("DATABASE_HOST")
    boa.Param(ctx, &p.DB.Password).SetNoFlag(true)
    boa.Param(ctx, &p.DB.Password).SetEnv("DATABASE_PASSWORD")
    return nil
},
```

Explicit names and environment bindings inside a named field are still prefixed. Set the complete desired value programmatically when you do not want that convention.

## Optional external sections

A pointer makes the whole group optional:

```go
type Params struct {
    Cache *cache.Config
}
```

BOA temporarily allocates `Cache` so Init can address `&p.Cache.Host`. After value sourcing it restores the pointer to `nil` unless CLI, environment, or config input populated a child. Defaults alone do not keep an optional group alive.

## Add config-file loading

If the file has the external struct's top-level shape, embed it in a wrapper that owns the path:

```go
type Params struct {
    external.Settings
    ConfigFile string `configfile:"true" optional:"true"`
}
```

Anonymous embedding keeps the external fields flat. `--config-file app.json` decodes into the wrapper, while external fields remain available as ordinary flags.

For a namespaced file shape, use a named field:

```go
type Params struct {
    ConfigFile string            `configfile:"true" optional:"true"`
    Database   external.Settings `json:"database" yaml:"database"`
}
```

The path can also be designated programmatically:

```go
type Params struct {
    ConfigFile string `optional:"true"`
    external.Settings
}

InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    boa.Param(ctx, &p.ConfigFile).SetConfigFile(true)
    return nil
},
```

A config-file field must be `string` or `[]string`. See [Configuration Files](configuration.md) for formats, overlay chains, and explicit loading.

## Tag-to-method mapping

| Tag behavior | Programmatic form |
|---|---|
| `descr` | `SetDescription(string)` |
| `name`, `short`, `env` | `SetName`, `SetShort`, `SetEnv` |
| `default` | `Field[T].SetDefault(T)` |
| `required`, `optional` | `SetRequired(bool)` or `SetRequiredFn` |
| `alts`, `strict` | `SetAlternatives`, `SetStrictAlts` |
| `min`, `max` | typed `SetMin`/`SetMax` or length variants |
| `pattern` | `SetPattern(string)` |
| `collection` | `SetCollection(boa.CollectionSlice/Array)` |
| `positional`, `persistent` | `SetPositional`, `SetPersistent` |
| `boa:"noflag"`, `boa:"noenv"` | `SetNoFlag`, `SetNoEnv` |
| `boa:"configonly"` | `SetNoFlag(true)` + `SetNoEnv(true)` |
| `boa:"ignore"` | `SetIgnored(true)` |
| `configfile` | `SetConfigFile(true)` |

Use typed `SetCustomValidator` for checks that cannot be expressed by tags.

## Reusable configuration helpers

Keep a policy reusable by accepting the live context and field owner:

```go
func ConfigureDatabase(ctx *boa.HookContext, db *dbconfig.Config) {
    boa.Param(ctx, &db.Host).SetDefault("localhost")
    boa.Param(ctx, &db.Port).SetDefault(5432)
    boa.Param(ctx, &db.Password).SetNoFlag(true)
    boa.Param(ctx, &db.Password).SetEnv("DATABASE_PASSWORD")
}
```

Call the helper from `InitFuncCtx` after the external struct has been placed in the command's actual parameter tree.

## Limits

- `boa.Param` only resolves fields belonging to the current command's live parameter instance.
- Unexported fields are not addressable and are skipped.
- Unsupported recursive structures need `boa:"ignore"` or a registered scalar handler at the recursion boundary. `boa:"configonly"` still traverses and validates the field, so it does not break recursion.
- A field marked ignored has no BOA mirror and therefore cannot be retrieved with `boa.Param`.
- Duplicate flattened field names still produce duplicate flag registration errors; prefer named composition when types may collide.

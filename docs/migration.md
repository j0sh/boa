# Migrating from GiGurra/boa

This guide covers the differences between [GiGurra/boa](https://github.com/GiGurra/boa) and this fork. It intentionally does not preserve migration history for older releases of the original project.

## Summary

| GiGurra/boa | j0sh/boa |
|---|---|
| Module `github.com/GiGurra/boa` | Module `github.com/j0sh/boa` |
| Go 1.25 | Go 1.27 |
| `CmdT[T]` plus exported erased `Cmd` | One public `Cmd[T]` |
| `GetParamT` / `ParamT[T]` and untyped `HookContext.GetParam` | `Param` / `*Field[T]` embedding `Parameter` |
| Typed method suffixes such as `SetDefaultT` | `SetDefault`, `SetCustomValidator`, `SetMin`, `SetMax` |
| `ConfigUnmarshal` compatibility field | `ConfigFormat` as the per-command override |
| Decoder repair/retry behavior | One authoritative decoder call |
| Long and shorthand tag aliases | Canonical tag names only |

Most command fields, struct shapes, hooks, config tags, and Cobra integration remain recognizable. The migration is primarily import replacement and a small number of mechanical API renames.

## Module and Go version

Update imports and the module dependency:

**Before:**

```go
import "github.com/GiGurra/boa/pkg/boa"
```

**After:**

```go
import "github.com/j0sh/boa/pkg/boa"
```

```bash
go get github.com/j0sh/boa@latest
go mod tidy
```

The fork requires Go 1.27 because its built-in JSON implementation uses the Go 1.27 `encoding/json/v2` API.

## Commands: CmdT becomes Cmd

The generic command is now the only public command type. The erased implementation is private.

**Before:**

```go
boa.CmdT[Params]{
    Use:    "serve",
    RunFunc: runServe,
}.Run()
```

**After:**

```go
boa.Cmd[Params]{
    Use:    "serve",
    RunFunc: runServe,
}.Run()
```

Code using the non-generic `boa.Cmd`, `CmdIfc`, or `CmdT.ToCmd()` should stay typed until converting directly to Cobra:

```go
cmd := boa.Cmd[Params]{Use: "serve", RunFunc: runServe}.ToCobra()
```

`CmdList` was removed. `SubCmds` is the single helper for heterogeneous typed commands:

```go
children := boa.SubCmds(
    boa.Cmd[ServeParams]{Use: "serve", RunFunc: runServe},
    boa.Cmd[DeployParams]{Use: "deploy", RunFunc: runDeploy},
)
```

## Field configuration

`boa.Param` replaces both `GetParamT` and `HookContext.GetParam`. The field pointer infers `T`, and `*Field[T]` embeds the general `Parameter` interface.

**Before:**

```go
port := boa.GetParamT(ctx, &p.Port)
port.SetDefaultT(8080)
port.SetMinT(1)
port.SetMaxT(65535)
port.SetCustomValidatorT(validatePort)

ctx.GetParam(&p.Host).SetDefault(boa.Default("localhost"))
```

**After:**

```go
port := boa.Param(ctx, &p.Port)
port.SetDefault(8080)
port.SetMin(1)
port.SetMax(65535)
port.SetCustomValidator(validatePort)

boa.Param(ctx, &p.Host).SetDefault("localhost")
```

For strings, slices, and maps, length bounds are explicit:

```go
boa.Param(ctx, &p.Name).SetMinLen(3)
boa.Param(ctx, &p.Tags).SetMaxLen(10)
```

The standalone `boa.Default` helper and `boa.HasValue(param)` were removed. Pass values directly to `SetDefault`; use `ctx.HasValue(&p.Field)` or `field.HasValue()`.

The untyped public interface was renamed from `Param` to `Parameter`. Most applications only see it when writing a `ParamEnricher` or iterating `ctx.AllMirrors()`.

## Canonical struct tags

The fork accepts one spelling for each tag. Replace aliases mechanically:

| GiGurra/boa alias | j0sh/boa canonical tag |
|---|---|
| `help`, `desc`, `description` | `descr` |
| `long` | `name` |
| `pos` | `positional` |
| `alternatives` | `alts` |
| `strict-alts` | `strict` |
| `req` | `required` |
| `opt` | `optional` |
| `boa:"nocli"` | `boa:"noflag"` |

Other current tags—including `short`, `env`, `default`, `alts`, `strict`, `min`, `max`, `pattern`, `persistent`, `collection`, `configfile`, `configonly`, `noenv`, and `ignore`—retain their canonical spelling. See [Parameters and Struct Tags](struct-tags.md#tag-reference).

## Config format overrides

The registry API remains the preferred path:

```go
boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
boa.RegisterConfigFormat(".toml", toml.Unmarshal)
```

The legacy `Cmd.ConfigUnmarshal` field was removed. Use a complete `ConfigFormat` override when one command must bypass extension dispatch.

**Before:**

```go
boa.CmdT[Params]{
    Use:            "app",
    ConfigUnmarshal: yaml.Unmarshal,
}
```

**After:**

```go
boa.Cmd[Params]{
    Use:          "app",
    ConfigFormat: boa.UniversalConfigFormat(yaml.Unmarshal),
}
```

`UnMarshalFromFileParam` was removed; use `LoadConfigFile`, `LoadConfigFiles`, or the automatic `configfile:"true"` field.

## Config decoding behavior

The fork treats each selected decoder as authoritative: it calls the decoder once for the target and preserves its values, custom methods, and errors. It no longer retries failed decodes through proxy structs or reconstructs third-party type systems.

Built-in JSON shares registered string parsers with flags and environment variables. For portable custom scalars:

- implement `encoding.TextUnmarshaler` on a type you own;
- use `boa.Text[T]` around a type you cannot change; or
- use `boa.RegisterType` when BOA should own the textual parser.

See [Config Decoding](config-decoding.md) for exact cross-format behavior.

## Validation and reload behavior

The public calls keep their names, but the fork makes their boundaries stricter:

- `Cmd.Validate()` validates only the current command, does not route to children, and skips every PreExecute and Run hook.
- `boa.Reload` rebuilds only the executed command, skips every PreExecute and Run hook including struct methods, and replays the captured invocation rather than reading `os.Args` again.
- Reloads through one `HookContext` are serialized. A successful reload refreshes its watched-file list; a failed reload preserves the previous list.
- Nested Init hooks run on the nested receiver, and config-file targets are resolved by field path rather than traversal order.

See [Lifecycle and Errors](lifecycle.md#validation-without-actions) and [Live Config Reload](live-reload.md).

## Other visible behavior changes

- A false default installed only by Boolean enrichment is omitted from help unless the user supplied the value.
- Enum completions suppress filesystem candidates.
- Subcommand-only roots retain Cobra's spelling suggestions.
- Empty JSON collection arguments are rejected instead of retaining an unparsed string.
- Recursive parameter groups return a construction error instead of recursing without a bound. Mark recursive config-only data ignored or register a scalar parser at the boundary.

## Migration checklist

1. Change the module import path to `github.com/j0sh/boa`.
2. Upgrade the project toolchain to Go 1.27.
3. Rename `CmdT[T]` to `Cmd[T]`; remove uses of the erased `Cmd`, `CmdIfc`, `ToCmd`, and `CmdList`.
4. Replace `GetParamT` and `HookContext.GetParam` with `boa.Param`.
5. Remove typed method suffixes and pass defaults directly rather than through `boa.Default`.
6. Replace tag aliases with canonical spellings.
7. Replace `ConfigUnmarshal` and `UnMarshalFromFileParam` if used.
8. Review custom decoder assumptions and any side effects in Init, PostCreate, or PreValidate hooks that will run during reload.
9. Run `go test ./...`, then exercise help, completion, config loading, validation-only paths, and reload behavior relevant to the application.

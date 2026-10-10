# Lifecycle and Errors

BOA separates command construction, value sourcing, validation, and action execution. Use the earliest hook that has the state you need.

## Execution order

| Phase | State available | Typical use |
|---|---|---|
| Init | Before flag setup | Configure fields, defaults, validators, completion |
| PostCreate | Cobra flags exist; arguments are not parsed | Inspect or customize generated flags |
| PreConfig | CLI/env values and defaults | Choose where to find config files |
| PostConfig | Config merged; paths and secrets not resolved | Set `basedir` or calculate defaults |
| PreValidate | Parsed values, resolved paths, and loaded secrets | Cross-field checks |
| Validation | Required/alternatives/bounds/pattern/custom validators | Automatic BOA pipeline |
| PreExecute | Values are valid | Establish action-specific resources |
| Run | Command action | Application logic |

Struct methods run before their corresponding command callbacks. `Validate()` and reload run setup, config, and validation hooks; they skip PreExecute and Run.

## Hook forms

Use command callbacks or the corresponding parameter-struct methods:

| Phase | Command fields | Struct methods |
|---|---|---|
| Init | `InitFunc`, `InitFuncCtx` | `Init()`, `InitCtx(ctx)` |
| PostCreate | `PostCreateFunc`, `PostCreateFuncCtx` | `PostCreate()`, `PostCreateCtx(ctx)` |
| PreConfig | `PreConfigFuncCtx` | — |
| PostConfig | `PostConfigFuncCtx` | — |
| PreValidate | `PreValidateFunc`, `PreValidateFuncCtx` | `PreValidate()`, `PreValidateCtx(ctx)` |
| PreExecute | `PreExecuteFunc`, `PreExecuteFuncCtx` | `PreExecute()`, `PreExecuteCtx(ctx)` |

All hooks return errors. The `Ctx` variants receive `*boa.HookContext`.

## Init: configure fields

Metadata that affects binding must be set during Init:

```go
boa.Cmd[Params]{
    Use: "serve",
    InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
        host := boa.Param(ctx, &p.Host)
        host.SetDefault("localhost")
        host.SetEnv("SERVER_HOST")

        port := boa.Param(ctx, &p.Port)
        port.SetDefault(8080)
        port.SetMin(1)
        port.SetMax(65535)
        return nil
    },
}
```

`boa.Param` returns a type-safe `*boa.Field[T]`. The complete field API is documented in [Parameters and Struct Tags](struct-tags.md#programmatic-field-configuration).

## PostCreate: customize Cobra flags

PostCreate runs after BOA has registered flags:

```go
PostCreateFunc: func(_ *Params, cmd *cobra.Command) error {
    flag := cmd.Flags().Lookup("verbose")
    flag.NoOptDefVal = "true"
    return nil
},
```

Use it for Cobra operations that require an existing flag. It is too late to change BOA metadata such as a field's name or environment binding.

## Config hooks: derive directories and defaults

Use `PreConfigFuncCtx` to set config paths or `basedir` from CLI/environment settings. Use `PostConfigFuncCtx` to set `basedir` or calculate defaults from config, before BOA resolves paths and reads secrets:

```go
PostConfigFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command, _ []string) error {
    if !ctx.HasInput(&p.DataDir) {
        p.DataDir = filepath.Join("data", p.Network)
    }
    return nil
},
```

`HasInput` preserves operator-supplied values; `HasValue` also counts defaults and application-generated values. See [source precedence](struct-tags.md#source-precedence) for empty-value behavior. Set `basedir` here; it cannot change in PreValidate.

The runnable [base-directory example](https://github.com/j0sh/boa/tree/main/internal/example_basedir) sets `basedir` to `data/<network>` using the loaded `Network` setting. It uses a `default` tag for the state filename and a PostConfig hook to choose `basedir`.

## PreValidate: inspect final values

PreValidate sees parsed and merged values before field validation:

```go
PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
    if p.StartPort > p.EndPort {
        return boa.NewUserInputErrorf("start port must not exceed end port")
    }
    return nil
},
```

Use this phase for cross-field input checks or the explicit configuration helpers described in [Configuration Files](configuration.md#explicit-file-and-byte-loading).

## PreExecute: prepare the action

PreExecute runs only after validation succeeds and immediately before Run. Use it for work that belongs to executing the action, such as opening a connection. It is intentionally skipped by `Cmd.Validate()` and `boa.Reload`.

## Run functions

Set exactly one run function per command:

| Function | Context | Returns error |
|---|---:|---:|
| `RunFunc` | no | no |
| `RunFuncCtx` | yes | no |
| `RunFuncE` | no | yes |
| `RunFuncCtxE` | yes | yes |

Configuring more than one is API misuse and fails command setup.

## Run and RunE behavior

| Method | Setup errors, including Init/PostCreate | Input and execution-hook errors | Ordinary action errors |
|---|---|---|---|
| `Run()` / `RunArgs()` | panic | print usage/error and exit 1 | panic |
| `RunE()` / `RunArgsE()` | return | return | return |

`RunE` does not recover panics from application code or invalid API use.

Use `Run()` at a normal CLI process boundary:

```go
func main() {
    boa.Cmd[Params]{Use: "app", RunFunc: run}.Run()
}
```

Use `RunE()` when another layer owns error policy:

```go
err := boa.Cmd[Params]{
    Use: "app",
    RunFuncE: func(p *Params, _ *cobra.Command, _ []string) error {
        return runApplication(p)
    },
}.RunE()
```

## User input errors

BOA wraps bad flags, missing arguments, required-field failures, invalid environment values, alternatives, bounds, patterns, and custom-validator failures as user input errors.

Create the same category in an application hook with `NewUserInputError` or `NewUserInputErrorf`. Detect it with `IsUserInputError`:

```go
err := cmd.RunArgsE(args)
if boa.IsUserInputError(err) {
    // Report a usage problem without treating it as an internal failure.
}
```

## ToCobra and ToCobraE

| Method | Result | Setup failures |
|---|---|---|
| `ToCobra()` | `*cobra.Command` | panic |
| `ToCobraE()` | `(*cobra.Command, error)` | return |

Both methods construct the command and run Init and PostCreate. Config loading and later phases run when the Cobra command executes.

Use `boa.Execute(cmd)` when you want BOA's usage/error output around an assembled Cobra tree.

## Validation without actions

`Cmd.Validate()` validates the current command without routing to children and skips all PreExecute and Run hooks:

```go
err := boa.Cmd[Params]{
    Use:    "app",
    RawArgs: []string{"--port", "8080"},
}.Validate()
```

Init, PostCreate, source loading with both config hooks, PreValidate, and field validation still run.

## Testing

Inject arguments rather than replacing process globals:

```go
func TestCommand(t *testing.T) {
    var got string
    err := boa.Cmd[Params]{
        Use: "app",
        RunFunc: func(p *Params, _ *cobra.Command, _ []string) {
            got = p.Name
        },
    }.RunArgsE([]string{"--name", "Ada"})

    if err != nil {
        t.Fatal(err)
    }
    if got != "Ada" {
        t.Fatalf("got %q", got)
    }
}
```

Use `Validate` when the test concerns only sourcing and validation. Use `ToCobraE` when a test needs Cobra's output buffers or command-tree APIs.

See [Live Config Reload](live-reload.md) for replay and watch behavior.

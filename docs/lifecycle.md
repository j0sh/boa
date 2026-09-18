# Lifecycle and Errors

BOA separates command construction, value sourcing, validation, and action execution. Use the earliest hook that has the state you need.

## Execution order

| Phase | State available | Typical use |
|---|---|---|
| Init | Field mirrors exist; flags are not bound | Configure metadata, defaults, validators, completion |
| PostCreate | Cobra flags exist; arguments are not parsed | Inspect or customize generated flags |
| Source loading | CLI, env, nested config, root config, defaults | Automatic BOA pipeline |
| PreValidate | Final parsed values are in the parameter struct | Cross-field checks, explicit config loading |
| Validation | Required/alternatives/bounds/pattern/custom validators | Automatic BOA pipeline |
| PreExecute | Values are valid | Establish action-specific resources |
| Run | Command action | Application logic |

Struct-method hooks run before the corresponding command function. `Init`, `PostCreate`, `PreValidate`, and validation run during `Validate` and live-reload reconstruction as applicable. PreExecute and Run are action hooks and are skipped by validation-only and reload operations.

## Hook forms

Every lifecycle phase has command function fields. Parameter structs may implement equivalent interfaces:

| Phase | Command fields | Struct methods |
|---|---|---|
| Init | `InitFunc`, `InitFuncCtx` | `Init()`, `InitCtx(ctx)` |
| PostCreate | `PostCreateFunc`, `PostCreateFuncCtx` | `PostCreate()`, `PostCreateCtx(ctx)` |
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

Both methods perform construction, including Init and PostCreate. PreValidate, validation, PreExecute, and Run happen when the resulting Cobra command executes.

Use `boa.Execute(cmd)` when you want BOA's usage/error output around an assembled Cobra tree.

## Validation without actions

`Cmd.Validate()` validates the current command without routing to children and skips all PreExecute and Run hooks:

```go
err := boa.Cmd[Params]{
    Use:    "app",
    RawArgs: []string{"--port", "8080"},
}.Validate()
```

Init, PostCreate, source loading, PreValidate, and field validation still run.

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

Live reload intentionally reuses only the setup and validation side of this lifecycle. See [Live Config Reload](live-reload.md#hook-behavior-on-reload) for the exact replay matrix.

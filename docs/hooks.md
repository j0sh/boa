# Lifecycle Hooks

BOA provides lifecycle hooks to customize behavior at different stages of command execution.

## Hook Execution Order

1. **Init** - Parameter mirrors exist, cobra flags not yet created
2. **PostCreate** - Cobra flags are now registered
3. **PreValidate** - After flags are parsed but before validation
4. **Validation** - Built-in parameter validation
5. **PreExecute** - After validation but before command execution
6. **Run** - The actual command execution

## Init Hook

Runs during initialization, after BOA creates internal parameter mirrors but before cobra flags are registered.

### Interface-based

```go
func (c *MyConfig) Init() error {
    // Initialize defaults, set up validators
    return nil
}

// With HookContext access
func (c *MyConfig) InitCtx(ctx *boa.HookContext) error {
    boa.Param(ctx, &c.Host).SetDefault("localhost")
    return nil
}
```

### Function-based

```go
boa.Cmd[Params]{
    Use: "cmd",
    InitFunc: func(params *Params, cmd *cobra.Command) error {
        return nil
    },
}

// With HookContext
boa.Cmd[Params]{
    Use: "cmd",
    InitFuncCtx: func(ctx *boa.HookContext, params *Params, cmd *cobra.Command) error {
        boa.Param(ctx, &params.Name).SetShort("n")
        return nil
    },
}
```

## PostCreate Hook

Runs after cobra flags are created but before arguments are parsed. Useful for inspecting or modifying cobra flags.

### Interface-based

```go
func (c *MyConfig) PostCreate() error {
    // Flags are now registered
    return nil
}

// With HookContext
func (c *MyConfig) PostCreateCtx(ctx *boa.HookContext) error {
    return nil
}
```

### Function-based

```go
boa.Cmd[Params]{
    Use: "cmd",
    PostCreateFuncCtx: func(ctx *boa.HookContext, params *Params, cmd *cobra.Command) error {
        flag := cmd.Flags().Lookup("my-flag")
        if flag != nil {
            // Inspect or modify flag
        }
        return nil
    },
}
```

## PreValidate Hook

Runs after parameters are parsed but before validation. Useful for loading config files explicitly (though the `configfile` struct tag handles this automatically — see [Advanced](advanced.md#config-file-loading)).

### Interface-based

```go
func (c *MyConfig) PreValidate() error {
    // Manipulate parameters before validation
    return nil
}

// With HookContext
func (c *MyConfig) PreValidateCtx(ctx *boa.HookContext) error {
    return nil
}
```

### Function-based

```go
boa.Cmd[Params]{
    Use: "cmd",
    PreValidateFunc: func(params *Params, cmd *cobra.Command, args []string) error {
        return nil
    },
}
```

## PreExecute Hook

Runs after validation but before the Run function. Use for setup like establishing connections.

### Interface-based

```go
func (c *MyConfig) PreExecute() error {
    // Setup resources
    return nil
}
```

### Function-based

```go
boa.Cmd[Params]{
    Use: "cmd",
    PreExecuteFunc: func(params *Params, cmd *cobra.Command, args []string) error {
        return nil
    },
}
```

## HookContext

`HookContext` provides access to the fields registered for the current command:

- `boa.Param(ctx, &params.Field)` returns a type-safe `*boa.Field[T]`.
- `ctx.HasValue(&params.Field)` reports whether a source supplied a value.
- `ctx.AllMirrors()` returns `[]boa.Parameter` for bulk inspection or configuration.

### Field Access

```go
func (c *ServerConfig) InitCtx(ctx *boa.HookContext) error {
    // Type-safe: SetDefault takes int, SetCustomValidator takes func(int) error
    portParam := boa.Param(ctx, &c.Port)
    portParam.SetDefault(8080)
    portParam.SetCustomValidator(func(port int) error {
        if port < 1 || port > 65535 {
            return fmt.Errorf("port must be between 1 and 65535")
        }
        return nil
    })
    return nil
}
```

`*boa.Field[T]` embeds `boa.Parameter`, so it combines type-safe operations with general metadata methods:

| Typed Methods | Description |
|---------------|-------------|
| `SetDefault(T)` | Set default value with type safety |
| `SetCustomValidator(func(T) error)` | Set typed validation function |
| `SetMin(T)`, `SetMax(T)` | Set numeric bounds |
| `SetMinLen(int)`, `SetMaxLen(int)` | Set length bounds on strings, slices, and maps |

| Pass-through Methods | Description |
|---------------------|-------------|
| `SetAlternatives([]string)` | Set allowed values |
| `SetStrictAlts(bool)` | Enable/disable strict validation |
| `SetAlternativesFunc(...)` | Set dynamic completion function |
| `SetEnv(string)` | Set environment variable |
| `SetShort(string)` | Set short flag |
| `SetName(string)` | Set flag name |
| `SetIsEnabledFn(func() bool)` | Dynamic visibility |
| `SetRequiredFn(func() bool)` | Dynamic required condition |

### Example: Programmatic Configuration

```go
type ServerConfig struct {
    Host     string
    Port     int
    LogLevel string
}

func (c *ServerConfig) InitCtx(ctx *boa.HookContext) error {
    hostParam := boa.Param(ctx, &c.Host)
    hostParam.SetDefault("localhost")
    hostParam.SetEnv("SERVER_HOST")

    portParam := boa.Param(ctx, &c.Port)
    portParam.SetDefault(8080)
    portParam.SetEnv("SERVER_PORT")

    logParam := boa.Param(ctx, &c.LogLevel)
    logParam.SetDefault("info")
    logParam.SetAlternatives([]string{"debug", "info", "warn", "error"})
    logParam.SetStrictAlts(true)

    return nil
}
```

### Example: Checking Parameter Sources at Runtime

```go
type Params struct {
    Host string `default:"localhost"`
    Port int    `optional:"true"`
}

func main() {
    boa.Cmd[Params]{
        Use: "server",
        RunFuncCtx: func(ctx *boa.HookContext, params *Params, cmd *cobra.Command, args []string) {
            if ctx.HasValue(&params.Port) {
                fmt.Printf("Starting on %s:%d\n", params.Host, params.Port)
            } else {
                fmt.Printf("Starting on %s (no port)\n", params.Host)
            }
        },
    }.Run()
}
```

!!! note
    You can only use one run function variant per command: `RunFunc`, `RunFuncCtx`, `RunFuncE`, or `RunFuncCtxE`.

## Error Handling in Hooks

All lifecycle hooks return errors. When using `Run()`, hook errors cause panics. When using `RunE()`, hook errors are returned for programmatic handling.

```go
err := boa.Cmd[Params]{
    Use: "cmd",
    InitFunc: func(p *Params, cmd *cobra.Command) error {
        return fmt.Errorf("init failed")
    },
}.RunE() // Returns error instead of panicking
```

For comprehensive coverage of error handling including `Run()` vs `RunE()`, error-returning run functions, and testing patterns, see [Error Handling](error-handling.md).

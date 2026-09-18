# Error Handling

BOA provides two execution modes: `Run()` for simple CLI apps, and `RunE()` for programmatic error handling.

## Run() vs RunE()

| Method | Returned setup errors (including Init/PostCreate) | Input and PreValidate/PreExecute errors | Ordinary RunFuncE/RunFuncCtxE errors |
|--------|-------------------------------------------------|---------------------------------------|------------------------------------|
| `Run()` / `RunArgs(args)` | Panic | Print usage/error, exit 1 | Panic |
| `RunE()` / `RunArgsE(args)` | Return | Return | Return |

API misuse that panics internally (such as configuring multiple run functions) and panics raised by user code remain panics in either mode. `RunE()` does not recover them. An action error wrapped with `NewUserInputError` follows the input-error behavior.

### Using Run()

`Run()` is the simplest way to execute a command. User input errors print a message and exit cleanly. See the table above for full behavior.

```go
func main() {
    boa.Cmd[Params]{
        Use: "app",
        RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
            // Your command logic
        },
    }.Run()
}
```

### Using RunE()

`RunE()` returns setup and execution errors for programmatic handling. It is useful for testing, embedding, or custom error handling; it does not recover panics.

```go
err := boa.Cmd[Params]{
    Use: "app",
    RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
        if p.Port < 1024 {
            return fmt.Errorf("port must be >= 1024")
        }
        return nil
    },
}.RunE()

if err != nil {
    log.Printf("Command failed: %v", err)
    os.Exit(1)
}
```

## Error Types

BOA handles four categories of errors (see table above for behavior):

### 1. Setup Errors

Command construction can return errors for invalid tag values (such as `default:"abc"` on an `int`), invalid positional ordering, and errors returned by Init/PostCreate hooks. `Run()` panics on these errors; `RunE()` returns them.

Some API misuse still panics in both modes, including setting multiple run functions or using a non-struct `Cmd` type. Unsupported fields may be ignored with a warning rather than rejected; mark intentionally excluded fields with `boa:"ignore"`.

```go
type Params struct {
    Port int `default:"not-a-number"` // RunE returns a setup error; Run panics
}
```

### 2. User Input Errors

Invalid input from the CLI user. With `Run()`: prints error and exits(1). With `RunE()`: returns error.

- Missing required parameters
- Invalid flag values (e.g., `--port abc` for an integer flag)
- Unknown flags (e.g., `--unknown-flag`)
- Invalid alternatives (enum validation failures)
- Custom validator failures
- Invalid environment variable values
- Missing positional arguments

```go
type Params struct {
    Name string `short:"n" required:"true"`
    Mode string `default:"fast" alts:"fast,slow"`
}

// User runs: myapp --mode=invalid
// Output: Error: invalid value for param 'mode': 'invalid' is not in the list of allowed values: [fast slow]
// Exit code: 1
```

#### Creating User Input Errors in Hooks

Use `NewUserInputError` or `NewUserInputErrorf` to return user input errors from hooks:

```go
boa.Cmd[Params]{
    Use: "app",
    PreValidateFunc: func(p *Params, cmd *cobra.Command, args []string) error {
        if p.StartPort > p.EndPort {
            return boa.NewUserInputErrorf("start port must be less than end port")
        }
        return nil
    },
}
```

#### Checking for User Input Errors

```go
err := cmd.RunArgsE([]string{"--invalid-flag"})
if boa.IsUserInputError(err) {
    // Handle user input error
}
```

### 3. Hook Errors

Init and PostCreate errors occur during construction; PreValidate and PreExecute errors occur during execution. `RunE()` returns both. `Run()` panics on construction errors and exits 1 on execution-hook errors. A panic raised inside a hook is not converted to an error.

```go
err := boa.Cmd[Params]{
    Use: "app",
    InitFunc: func(p *Params, cmd *cobra.Command) error {
        return fmt.Errorf("initialization failed")
    },
}.RunE()
// err: "error in InitFunc: initialization failed"
```

### 4. Runtime Errors

Errors from your `RunFuncE`. Behavior depends on `Run()` vs `RunE()` - see table.

```go
err := boa.Cmd[Params]{
    Use: "app",
    RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
        return fmt.Errorf("something went wrong")
    },
}.RunArgsE([]string{"--name", "test"})
// err: "something went wrong"
```

## Error-Returning Run Functions

| Non-Error Variant | Error Variant | Description |
|-------------------|---------------|-------------|
| `RunFunc` | `RunFuncE` | Basic run function |
| `RunFuncCtx` | `RunFuncCtxE` | Run function with HookContext access |

## ToCobra() vs ToCobraE()

| Method | Returns | Returned construction errors |
|--------|---------|------------------------------|
| `ToCobra()` | `*cobra.Command` | Panic |
| `ToCobraE()` | `(*cobra.Command, error)` | Return |

These methods run Init and PostCreate hooks while building the command. PreValidate, PreExecute, and action hooks run later when the Cobra command executes. Neither method recovers panics.

## Testing

Use `RunE()` and `RunArgsE()` for testing:

```go
func TestMyCommand_InvalidPort(t *testing.T) {
    err := boa.Cmd[Params]{
        Use: "app",
        RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
            if p.Port < 1024 {
                return fmt.Errorf("port must be >= 1024")
            }
            return nil
        },
    }.RunArgsE([]string{"--port", "80"})

    if err == nil {
        t.Fatal("expected error for port < 1024")
    }
}
```

## Only One Run Function

You can only set one run function per command. Setting multiple causes a setup error (panic):

```go
// This will panic - can't use both RunFunc and RunFuncE
boa.Cmd[Params]{
    Use:      "app",
    RunFunc:  func(p *Params, cmd *cobra.Command, args []string) {},
    RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error { return nil },
}
```

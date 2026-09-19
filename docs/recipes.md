# Recipes and Runnable Examples

The repository's complete examples live under `internal/example*` and are compiled by CI. This page keeps the documentation focused on the part that changes from recipe to recipe.

## Runnable examples

| Example | Demonstrates |
|---|---|
| [`example_readme_minimum`](https://github.com/j0sh/boa/tree/main/internal/example_readme_minimum) | Minimal typed command |
| [`example_readme_subcommands`](https://github.com/j0sh/boa/tree/main/internal/example_readme_subcommands) | Heterogeneous typed subcommands |
| [`example_readme_composition`](https://github.com/j0sh/boa/tree/main/internal/example_readme_composition) | Embedded and named struct composition |
| [`example_readme_slices`](https://github.com/j0sh/boa/tree/main/internal/example_readme_slices) | Slice flags and arguments |
| [`example_readme_conditional`](https://github.com/j0sh/boa/tree/main/internal/example_readme_conditional) | Conditional requirements |
| [`example_readme_config_file`](https://github.com/j0sh/boa/tree/main/internal/example_readme_config_file) | Automatic JSON config loading |
| [`example_access_to_cobra`](https://github.com/j0sh/boa/tree/main/internal/example_access_to_cobra) | Cobra access from hooks |
| [`example_raw_params_ctx`](https://github.com/j0sh/boa/tree/main/internal/example_raw_params_ctx) | Programmatic field policy |
| [`example_custom_config_format`](https://github.com/j0sh/boa/tree/main/internal/example_custom_config_format) | Full custom format and key-presence probe |
| [`example_trivial`](https://github.com/j0sh/boa/tree/main/internal/example_trivial) | Smallest practical command |
| [`example1`](https://github.com/j0sh/boa/tree/main/internal/example1) | Basic flags and output |

## Optional value versus zero

Use a pointer when the application must distinguish absence from an explicit zero:

```go
type Params struct {
    Retries *int
}

if p.Retries == nil {
    useAutomaticRetries()
} else {
    retryExactly(*p.Retries)
}
```

## Repeat a flag without CSV splitting

```go
type Params struct {
    Header []string `collection:"array"`
}
```

```text
app --header 'Accept: text/plain, text/html' --header 'X-Debug: true'
```

Each occurrence is one element. Without `collection:"array"`, flat slices use comma-separated slice semantics.

## Load a token from the environment or a file

Use an environment variable during local development and a mounted secret file in deployment:

```go
type Params struct {
    Token     string `secret:"true" env:"TOKEN"`
    TokenFile string `secretfor:"Token"`
}
```

```sh
# Local development
TOKEN=example-token app

# Deployment with a mounted secret file
app --token-file /run/secrets/api_token
```

Both invocations populate `p.Token` for your command handler. Supplying both sources causes an error. File contents are preserved exactly, including trailing newlines.

When creating a secret file, use `echo -n "$TOKEN" > token` or the more portable `printf '%s' "$TOKEN" > token` to avoid appending a newline to secrets such as API keys, access tokens, or passwords.

See [Secrets and secret files](struct-tags.md#secrets-and-secret-files) for config restrictions, validation, and reload behavior.

## Dynamic completion

```go
InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    boa.Param(ctx, &p.Region).SetAlternativesFunc(
        func(_ *cobra.Command, _ []string, prefix string) []string {
            return matchingRegions(prefix)
        },
    )
    return nil
},
```

Use `Cmd.ValidArgsFunc` for command-level positional completion. Static `alts` values automatically provide flag completion and, unless `strict:"false"`, validation.

## Conditional required fields

```go
InitFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command) error {
    boa.Param(ctx, &p.File).SetRequiredFn(func() bool {
        return p.Mode == "file"
    })
    boa.Param(ctx, &p.URL).SetRequiredFn(func() bool {
        return p.Mode == "http"
    })
    return nil
},
```

## Custom scalar

For an application-owned type, standard text interfaces are the most portable option:

```go
type Level string

func (l *Level) UnmarshalText(text []byte) error {
    switch Level(text) {
    case "debug", "info", "warn", "error":
        *l = Level(text)
        return nil
    default:
        return fmt.Errorf("unknown level %q", text)
    }
}
```

BOA discovers `encoding.TextUnmarshaler` for flags and environment variables. Supporting config decoders use the same interface. See [Config Decoding](config-decoding.md).

For a type you cannot modify, register a parser and formatter before constructing commands:

```go
boa.RegisterType[SemVer](boa.TypeDef[SemVer]{
    Parse:  ParseSemVer,
    Format: func(v SemVer) string { return v.String() },
})
```

## Mix BOA and Cobra

```go
root := &cobra.Command{Use: "tool"}
root.AddCommand(
    legacyCommand,
    boa.Cmd[ServeParams]{Use: "serve", RunFunc: runServe}.ToCobra(),
)
```

The inverse works too: a BOA parent's `SubCmds` accepts native `*cobra.Command` values. See [Cobra Interoperability](cobra-interop.md).

## Test a command

```go
func TestServe(t *testing.T) {
    var got int
    err := boa.Cmd[ServeParams]{
        Use: "serve",
        RunFunc: func(p *ServeParams, _ *cobra.Command, _ []string) {
            got = p.Port
        },
    }.RunArgsE([]string{"--port", "9090"})

    if err != nil {
        t.Fatal(err)
    }
    if got != 9090 {
        t.Fatalf("got %d", got)
    }
}
```

Use `Cmd.Validate()` when the action itself should not run. [Lifecycle and Errors](lifecycle.md#testing) covers the available test surfaces.

## More focused references

- [Parameters and Struct Tags](struct-tags.md): field types, validation, enrichment, and programmatic setters
- [Configuration Files](configuration.md): nested files, overlays, formats, discovery, and dumping
- [External Structs](external-structs.md): tagless and third-party configurations
- [Live Config Reload](live-reload.md): validated snapshot publication

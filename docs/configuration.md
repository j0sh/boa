# Configuration Files

BOA loads JSON by default and can dispatch additional formats by file extension. Automatic loading participates in BOA's normal precedence rules; explicit helpers are available when the application owns the loading policy.

## Automatic loading

Mark a `string` field with `configfile:"true"`. Its value names a file to decode into the enclosing struct:

```go
type Params struct {
    ConfigFile string `configfile:"true" optional:"true" default:"config.json"`
    Host       string `env:"HOST" default:"localhost"`
    Port       int    `env:"PORT" default:"8080"`
    Routes     []Route `boa:"configonly"`
}
```

The path itself can come from a flag, environment variable, or default. An empty path is skipped. Values loaded from the file remain below CLI and environment values:

```text
CLI > environment > root config > nested config > default > zero value
```

Use `boa:"configonly"` for fields that should still be mirrored and validated but must not be exposed through flags or environment variables. Use `boa:"noconfig"` for fields that may be available from other sources but should cause an error when present in a config file. Use `boa:"ignore"` for opaque data the decoder may populate but BOA should not process.

## Nested config files

A nested struct can own its own config path:

```go
type Database struct {
    ConfigFile string `configfile:"true" optional:"true"`
    Host       string `default:"localhost"`
    Port       int    `default:"5432"`
}

type Params struct {
    ConfigFile string   `configfile:"true" optional:"true" default:"app.json"`
    DB         Database
}
```

Nested files load first. The root file then overrides any overlapping nested values. CLI and environment sources still win over both.

## Overlay chains

A `[]string` config-file field creates a left-to-right overlay chain:

```go
type Params struct {
    ConfigFiles []string `configfile:"true" optional:"true"`
    Host        string   `optional:"true"`
    Port        int      `optional:"true"`
}

// app --config-files base.json,production.json
```

Later files replace keys they mention; absent keys preserve earlier values. Collection behavior follows the selected decoder. With built-in JSON, slices are replaced and map members merge according to `encoding/json` semantics. Empty path entries are skipped.

Each nested struct may have its own overlay chain. All nested chains load before the root chain.

## Registering formats

JSON is built in. Register other formats once, before constructing commands:

```go
boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
boa.RegisterConfigFormat(".yml", yaml.Unmarshal)
boa.RegisterConfigFormat(".toml", toml.Unmarshal)
```

Format selection is per file, so one binary—and even one overlay chain—may accept several formats.

When an unregistered extension falls back to JSON, field-name matching uses `json` tags too.

`RegisterConfigFormat` uses the decoder both for the target value and for a key-presence probe. Presence tracking lets BOA distinguish “the file explicitly supplied the default value” from “the file omitted this field,” including inside optional pointer groups.

The same key-presence probe enforces `boa:"noconfig"` before the target decoder runs. If a custom `ConfigFormat` has no `KeyTree`, loading a target with an applicable `noconfig` field fails closed. Explicit decoder functions passed to `LoadConfigFile`, `LoadConfigFiles`, or `LoadConfigBytes` are used for the probe as well as the target and therefore must support decoding into `map[string]any` when `noconfig` is present.

If a format permits repeated object members, its `KeyTree` must preserve all nested keys across those occurrences. The built-in JSON probe does this so a later object cannot hide a forbidden key in an earlier one.

Most parsers can decode into `map[string]any` and need no extra work. For a parser that only understands concrete structs, register both operations:

```go
boa.RegisterConfigFormatFull(".kv", boa.ConfigFormat{
    Unmarshal: kvUnmarshal,
    KeyTree:   kvKeys,
})
```

The tested [`internal/example_custom_config_format`](https://github.com/j0sh/boa/tree/main/internal/example_custom_config_format) demonstrates this uncommon full form.

## Per-command format override

`Cmd.ConfigFormat` bypasses extension dispatch for that command:

```go
boa.Cmd[Params]{
    Use:          "ingest",
    ConfigFormat: boa.UniversalConfigFormat(custom.Unmarshal),
}
```

Resolution for a load is:

1. the command's `ConfigFormat` override;
2. a registered format matching the extension;
3. built-in JSON.

Prefer the registry when an application accepts normal filename extensions. Use the per-command override for application-specific formats and tests.

## Explicit file and byte loading

Use the public helpers when paths or data are discovered by application logic:

```go
PreValidateFunc: func(p *Params, _ *cobra.Command, _ []string) error {
    return boa.LoadConfigFiles(
        []string{"/etc/myapp/config.json", "./config.local.json"},
        p,
        nil,
    )
},
```

The helpers are:

| Helper | Purpose |
|---|---|
| `LoadConfigFile(path, target, decoder)` | Load one file |
| `LoadConfigFiles(paths, target, decoder)` | Load a left-to-right chain |
| `LoadConfigBytes(data, ext, target, decoder)` | Decode embedded, remote, stdin, or test data |

A non-nil decoder argument overrides registry selection. Otherwise file extension or `ext` selects the registered format, with JSON as the fallback. Empty paths and empty byte slices are no-ops.

When called from `PreValidateFunc`, CLI and environment values retain their precedence. Explicit helper calls are not automatically added to the live-reload watch list; register file paths with `ctx.WatchConfigFile(path)` in a context-aware hook.

## Dumping configuration

BOA provides two output models:

| API | Output |
|---|---|
| `DumpConfigBytes`, `DumpConfigFile` | Every exported field, including zero values |
| `HookContext.DumpBytes`, `HookContext.DumpFile` | Only fields for which `HasValue` is true |

Source-aware dumping includes defaults so a saved config pins the current behavior across future application upgrades. It omits untouched zero values, the config-file path field itself, and `boa:"noconfig"` fields. The intentionally naive `DumpConfig*` helpers serialize the raw struct and do not enforce BOA source policies.

JSON marshaling is built in. Register a marshaler for other formats:

```go
boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
boa.RegisterConfigMarshaler(".yaml", yaml.Marshal)
```

Field names follow format-appropriate tags such as `json`, `yaml`, `toml`, and `hcl`.

## Automatic discovery with boaviper

The optional `pkg/boaviper` package searches conventional locations and assigns the discovered path before BOA binds fields:

```go
boa.Cmd[Params]{
    Use:     "myapp",
    InitFunc: boaviper.AutoConfig[Params]("myapp"),
}
```

It checks registered extensions at each location. Explicit command-line config paths still override discovery. Pass additional directories to `boaviper.AutoConfig("myapp", paths...)` for custom search paths, and use `boaviper.SetEnvPrefix` with the normal enricher chain for namespaced environment variables.

## Decoder contracts and custom values

Config decoders own their native behavior, custom methods, and errors. BOA does not retry failed third-party decodes with proxy types. See [Config Decoding](config-decoding.md) for built-in JSON, `encoding.TextUnmarshaler`, `boa.Text[T]`, and cross-format custom scalar guidance.

For live services, [Live Config Reload](live-reload.md) describes how automatic files are tracked and revalidated.

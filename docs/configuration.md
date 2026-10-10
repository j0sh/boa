# Configuration Files

BOA loads JSON by default and can dispatch additional formats by file extension. Automatic loading participates in BOA's normal precedence rules; explicit helpers are available when the application owns the loading policy.

## Automatic loading

Mark a `string` field with `configfile` to decode a file into the enclosing struct. This example allows the default config file to be missing:

```go
type Params struct {
    ConfigFile string `configfile:"optional-default" default:"config.json"`
    Host       string `env:"HOST" default:"localhost"`
    Port       int    `env:"PORT" default:"8080"`
    Routes     []Route `boa:"configonly"`
}
```

The path can come from CLI, environment, defaults, application code, or a config file loaded before this field. An empty path is skipped.

| Tag | Missing-file behavior |
|---|---|
| `configfile:"true"` | Fail |
| `configfile:"optional"` | Skip any missing path |
| `configfile:"optional-default"` | Skip paths matching a resolved default; fail for other paths |

An explicitly supplied path matching the resolved default is also skipped when missing. Existing paths must be regular files; unreadable files, invalid configs, and other filesystem errors still fail. File existence is independent of [parameter requiredness](struct-tags.md#required-and-optional).

Values loaded from the file remain below CLI and environment values:

```text
CLI > environment > root config > nested config > default > zero value
```

Use [`basedir`](struct-tags.md#base-directories) to set the directory for relative paths. With [`basepath:"source"`](struct-tags.md#source-relative-paths), paths read from config are relative to the config file. [Config hooks](lifecycle.md#config-hooks-derive-directories-and-defaults) can set `basedir` and calculate defaults from loaded settings.

`boa:"configonly"` disables flags and environment variables for a field while keeping config loading and validation. `boa:"noconfig"` rejects config files that contain the field. `boa:"ignore"` leaves the field to the config decoder, with no flags, environment variables, or BOA validation.

Fields marked `secret:"true"` cannot appear in config files. To store a path to a secret file instead, add a string field in the same struct tagged `secretfor:"FieldName"`, where `FieldName` names the secret field. See [Secrets and secret files](struct-tags.md#secrets-and-secret-files) for examples.

## Rejecting unknown fields

Set `RejectUnknown: true` on `boa.Cmd[Params]` to reject unknown fields in automatically loaded files, including nested files, overlays, and reloads. The default is permissive.

Checks follow format tags and include structs inside collections. Errors identify the file and field path. Standalone `LoadConfig*` helpers and CLI/environment decoding are unaffected.

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

Later files replace keys they mention; absent keys preserve earlier values. Collection behavior follows the selected decoder. With built-in JSON, slices are replaced and map members merge according to `encoding/json` semantics. Empty path entries are skipped. The missing-file policy above applies to each path; `optional-default` matches against any path in the default list.

Each nested struct may have its own overlay chain. All nested chains load before the root chain. If strict checking rejects a file, earlier files remain applied.

## Registering formats

JSON is built in. Register other formats once, before constructing commands:

```go
boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
boa.RegisterConfigFormat(".yml", yaml.Unmarshal)
boa.RegisterConfigFormat(".toml", toml.Unmarshal)
```

Format selection is per file, so one binary—and even one overlay chain—may accept several formats.

When an unregistered extension falls back to JSON, field-name matching uses `json` tags too.

Automatic config loading requires `ConfigFormat.KeyTree` to identify supplied fields for `HasInput`, path resolution, and config restrictions. `RegisterConfigFormat` supplies it using the decoder's `map[string]any` support. For a command-level decoder, use `UniversalConfigFormat` as shown below.

A decoder that only accepts concrete structs needs a separate key probe. Missing or failed probes reject the load before decoding:

```go
boa.RegisterConfigFormatFull(".kv", boa.ConfigFormat{
    Unmarshal: kvUnmarshal,
    KeyTree:   kvKeys,
})
```

See [`ConfigFormat.KeyTree`](https://pkg.go.dev/github.com/j0sh/boa/pkg/boa#ConfigFormat) for the probe contract and [`internal/example_custom_config_format`](https://github.com/j0sh/boa/tree/main/internal/example_custom_config_format) for a complete example.

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

A non-nil decoder argument overrides registry selection. Otherwise file extension or `ext` selects the registered format, with JSON as the fallback. Empty paths and empty byte slices are no-ops. These helpers fail on missing files and decode values without resolving paths or creating directories.

With `boa:"noconfig"` fields, a decoder override must also support `map[string]any` for key inspection.

When called from `PreValidateFunc`, CLI and environment values retain their precedence. Explicit helper calls are not automatically added to the live-reload watch list; register file paths with `ctx.WatchConfigFile(path)` in a context-aware hook.

## Dumping configuration

BOA provides two output models:

| API | Output |
|---|---|
| `DumpConfigBytes`, `DumpConfigFile` | Every exported field, including zero values |
| `HookContext.DumpBytes`, `HookContext.DumpFile` | Only fields for which `HasValue` is true |

The `HookContext` helpers include defaults so a saved config pins the current behavior across future application upgrades. They omit untouched zero values, the config-file path field itself, and `boa:"noconfig"` fields. They include `basedir` when set or given a default, unless explicitly excluded. The `DumpConfig*` helpers serialize the raw struct without applying BOA config restrictions.

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

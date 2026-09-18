# Config decoding and custom values

Boa shares scalar parsers between CLI flags, environment variables, default tags,
and its built-in JSON decoder. For example, a `time.Duration` field accepts
`--timeout 2.5h`, `TIMEOUT=2.5h`, and `{"timeout":"2.5h"}`.

## Built-in JSON

The JSON decoder uses Go 1.27's `encoding/json/v2` hooks with `encoding/json`
compatibility options. Strings for exactly registered types pass through the
registered parser, including values in nested structs, pointers, slices, and
maps. Numeric durations retain their nanosecond representation. Plain duration
fields still dump as numeric nanoseconds.

Other JSON values retain the native decoder's behavior. Custom `UnmarshalJSON`
methods own their entire value and run once. A method that wants Boa's parsing
inside its object can delegate to `boa.UnmarshalJSON` using the usual method-free
alias:

```go
func (p *Params) UnmarshalJSON(data []byte) error {
    type plain Params
    if err := boa.UnmarshalJSON(data, (*plain)(p)); err != nil {
        return err
    }
    if p.Limit > 10 {
        return fmt.Errorf("limit exceeds 10")
    }
    return nil
}
```

To configure a stricter JSON decoder, compose `boa.JSONUnmarshalers()` with
`jsonv2.WithUnmarshalers` and `jsonv2.RejectUnknownMembers(true)`.

Use duration strings with units. The legacy `json:",string"` convention for
quoted numeric durations is not part of Boa's textual duration syntax; use an
unquoted number for nanoseconds.

## Other formats

Register a decoder with `boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)` or
set `Cmd.ConfigFormat`. Boa calls the decoder once for the target and preserves
its values, custom methods, and errors. An optional independent `KeyTree` probe
tracks which keys were present; its scalar values are never used for conversion.

Native support varies by decoder. For example, yaml.v3 and BurntSushi TOML
support duration strings directly; pelletier TOML v2.2.4 needs a text adapter.
Boa does not detect parser names or reconstruct their type systems.

For a type you own, implement `encoding.TextUnmarshaler`. Boa discovers this
method for flags and env vars, and JSON/YAML/TOML decoders that support the
interface use it directly. Implement `encoding.TextMarshaler` as well for
round-tripping and default display.

For a type you cannot change, use the generic adapter:

```go
type Params struct {
    Timeout boa.Text[time.Duration] `json:"timeout" yaml:"timeout" toml:"timeout" env:"TIMEOUT" default:"30s"`
}

// p.Timeout.Value is a time.Duration.
```

`Text[T]` uses Boa's parser for `T`, including `RegisterType` registrations.
It reads and writes textual config values. For application-owned types, using
the standard encoding interfaces avoids a registry altogether. Register custom
types before constructing commands or decoding config, not concurrently with
running commands.

Cross-field checks can live in a native format's custom unmarshaler. Put checks
that must see the final CLI/env/config combination in a Boa PreValidate hook or
parameter validator, since config decoding happens before final source merging.

# In-memory defaults and validation

Use `ParseConfigBytes` to apply defaults, decode, and validate without running a
command:

```go
cfg, err := boa.ParseConfigBytes[Config](data, toml.Unmarshal)
```

The decoder determines the format; there is no extension argument. BOA calls
`Defaults[Config]()`, then the decoder with `data` and `&cfg`, then
`Validate(cfg)`. It stops on the first error, returning that error unchanged and
the value constructed so far. A nil decoder is an error. The decoder controls
key, type, and omission handling.

## Separate operations

Use `Defaults` and `Validate` independently when you need a different loading
policy or field configuration:

```go
cfg, err := boa.Defaults[Config]()
if err != nil {
    return err
}
if err := toml.Unmarshal(data, &cfg); err != nil {
    return err
}
return boa.Validate(cfg)
```

To validate persisted data without adding defaults, decode into a zero value
and call `Validate` alone. `LoadConfigBytes` only decodes; it does not apply
defaults or validate.

`Defaults` creates a fresh struct. Decode onto it so explicit zero, false,
empty, or nil values can replace defaults. Omitted values follow the decoder's
semantics.

| Default scope | Behavior |
|---|---|
| Nested value structs | Fields receive defaults |
| Nil pointer groups | Stay nil unless explicitly defaulted |
| Collections | Defaults are whole values; elements receive no additional overlay |
| Mutable values | Storage is independent between calls |

## Validation

`Validate` accepts a struct or a non-nil pointer to one and does not mutate it.
Presence is based on the Go value, so an omitted scalar cannot be distinguished
from an explicit zero after decoding.

| Value | Presence |
|---|---|
| Scalar, including `0`, `false`, or `""` | Present |
| Nil pointer, map, slice, or interface | Absent |
| Non-nil pointer or collection, including an empty collection | Present |
| Value struct | Present; fields are traversed |

`required` checks presence; absence does not itself mean a field is optional.
Flat slices and interface fields are required by default. Tags and the global
setting can override requiredness; see [Required and optional](struct-tags.md#required-and-optional).
Unlike command loading, `Validate` does not treat a default tag as a source: a
default does not satisfy an absent required value. Optional absent values skip
rules and traversal. Use `min`, `pattern`, or a custom validator to reject empty
or invalid values.

Registered and text scalar types, including root types, are treated as scalars.
Ignored and disabled fields are skipped.

| Type | Rules and traversal |
|---|---|
| String, slice, or map | Min/max bounds apply to length |
| Slice | Alternatives check each element; nested structs are traversed |
| Array | Nested structs are traversed; min/max bounds are ignored, and alternatives compare the whole formatted value |
| Map | Nested structs are traversed; non-nil maps with non-string keys produce `boa.CodeSchema` |

Both `Defaults` and `Validate` accept `ConfigureField` callbacks after tags are
parsed. Callbacks receive a Go field path and the public `Parameter` API. For a
`Config` with a `Port int` field, use `Field[T]` for a typed validator:

```go
err := boa.Validate(cfg, func(path string, p boa.Parameter) {
    if path == "Port" {
        p.SetMin(1024)
        field := boa.Field[int]{Parameter: p}
        field.SetCustomValidator(validPort)
    }
})
```

Callbacks can set defaults for `Defaults` or constraints for `Validate`. Setting
a default during validation does not make an absent value present. Validators
receive field copies; traversal still inspects the original values.

These operations do not automatically read environment variables or files,
resolve paths or secrets, enforce command HTTPBind policy, or run lifecycle
hooks. Decoders, callbacks, parsers, and validators are application code and can
have their own side effects.

## Errors

Use `errors.As` to inspect a `*boa.ValidationError`:

```go
var validation *boa.ValidationError
if errors.As(err, &validation) {
    for _, field := range validation.Fields {
        fmt.Println(field.Path, field.Code, field.Err)
    }
}
```

Paths use Go field names, dots, indices, and quoted map keys, such as
`Apps[2].HTTP.Port` and `Groups["west"].Port`. Custom causes support `errors.Is`.

| Code | Meaning |
|---|---|
| `boa.CodeRequired` | Required value is absent |
| `boa.CodeAlternatives` | Value is not an allowed alternative |
| `boa.CodeMin`, `boa.CodeMax` | Value or collection length is out of bounds |
| `boa.CodePattern` | Pattern check failed |
| `boa.CodeCustom` | Custom validator failed |
| `boa.CodeSchema` | Invalid metadata or input shape |
| `boa.CodeLimit` | Traversal or copying exceeded a depth or work limit |

Validation reports the first failed rule per field. Field-local depth and schema
errors do not suppress sibling errors. Limits are:

| Limit | Behavior |
|---|---|
| 100 errors | `Truncated` indicates the error-count cap was reached |
| 128 recursive traversal or copy steps | Includes pointer and interface dereferences |
| 10,000 traversal steps | Caps total traversal work |

Cyclic references are not revisited on the same active branch; a root
self-cycle is visited once.

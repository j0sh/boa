# In-memory defaults and validation

Use `ParseConfigBytes` to apply defaults, decode, and validate without a command:

```go
cfg, err := boa.ParseConfigBytes[Config](data, toml.Unmarshal)
```

The decoder determines the format; there is no extension argument. BOA calls
`Defaults[Config]()`, the decoder with `data` and `&cfg`, then `Validate(cfg)`.
It stops on the first error, returning it unchanged with the value built so far.
A nil decoder is an error.

The decoder controls keys, types, omission, and collection merging. Explicit
zero, false, empty string, and nil values replace defaults when the decoder
assigns them. For example, JSON `{}` retains entries in a non-nil default map.

## Separate operations

Use `Defaults` and `Validate` independently for another loading policy or field
configuration:

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

To validate persisted data without adding defaults, decode into a zero value and
call `Validate` alone. `LoadConfigBytes` only decodes.

`Defaults` returns a fresh struct with independent mutable values. Nested value
structs receive defaults; nil pointer groups stay nil unless explicitly
defaulted. Collection defaults are whole values: slice, map, and array elements
receive no additional default overlay.

## Validation

`Validate` accepts a struct or non-nil pointer to one. It does not normalize or
mutate the input. Presence depends on the Go value:

| Value | Presence |
|---|---|
| Scalar, including `0`, `false`, or `""` | Present |
| Nil pointer, map, slice, or interface | Absent |
| Non-nil pointer or collection, including an empty collection | Present |
| Value struct | Present; fields are traversed |

An omitted scalar cannot be distinguished from an explicit zero. `required`
checks presence; use `min`, `pattern`, or a custom validator to reject empty or
invalid values. Flat slices are required by default; pointers, maps, and
interfaces are optional. See [Required and optional](struct-tags.md#required-and-optional).
`Validate` ignores default tags for presence. After the required check, nil
values skip remaining rules and traversal.

Root structs are traversed; registered and text scalar fields are opaque.
Ignored and disabled fields are skipped.

| Type | Rules and traversal |
|---|---|
| String, slice, or map | Min/max bounds apply to length |
| Slice | Alternatives check each element; nested structs are traversed |
| Array | Nested structs are traversed; min/max are ignored and alternatives check the whole formatted value |
| Map | Nested structs are traversed; non-nil maps with non-string keys produce `boa.CodeSchema` |

`Defaults` and `Validate` accept `ConfigureField` callbacks with a Go field path
and a `Parameter`. For a `Config` with a `Port int` field:

```go
err := boa.Validate(cfg, func(path string, p boa.Parameter) {
    if path == "Port" {
        p.SetMin(1024)
        field := boa.Field[int]{Parameter: p}
        field.SetCustomValidator(validPort)
    }
})
```

Tags establish the baseline; explicit callback settings override defaults,
requiredness, and rules. Callbacks can target collection element paths. Setting
a default during validation does not make an absent value present. Custom
validators receive copies; bounds, patterns, and nested fields are checked on
original values, so changes to a copy cannot hide invalid data.

These operations do not automatically read environment variables or files,
resolve paths or secrets, enforce command HTTPBind policy, or run hooks.
Decoders, callbacks, parsers, and validators may have their own side effects.

## Command validation

Commands use the same rules and traversal, including nested collections, arrays,
and concrete interface values. With `Rows` a slice and `Groups` a map of
structs, these values fail:

| Field rule | Rejected value |
|---|---|
| `Port int` with `min:"1"` | `Rows[0].Port = 0` |
| `Mode string` with `alts:"tcp,http"` | `Groups["west"].Mode = "other"` |
| `Child *Options` with `required:"true"` | `Rows[0].Child = nil` |

Command requiredness uses source presence: an explicitly supplied nil secret
`*url.URL` counts as supplied. After that check, nil values skip other rules.
Explicitly required pointer groups are checked too.

Tags establish command defaults and rules before `Init` hooks; explicit settings
in those hooks override them, and `ParamEnrich` can make later changes. Command
parameter configuration applies to registered parameters, including collection
fields and ordinary nested-group fields; rules for individual collection elements
come from their tags. Source loading, conversion, environment, files, secrets,
and hooks remain outside validation; string-backed conversion runs before config
loading and again after `PreValidate`.

## Errors and limits

Command errors display the first failure; `errors.As` retrieves the full
`*boa.ValidationError` for either command or in-memory validation:

```go
var validation *boa.ValidationError
if errors.As(err, &validation) {
    for _, field := range validation.Fields {
        fmt.Println(field.Path, field.Code, field.Err)
    }
}
```

Paths use Go field names, dots, indices, and quoted map keys, such as
`Apps[2].HTTP.Port` and `Groups["west"].Port`; existing flat command names are
retained. Custom causes support `errors.Is`.

| Code | Meaning |
|---|---|
| `boa.CodeRequired` | Required value is absent |
| `boa.CodeAlternatives` | Value is not an allowed alternative |
| `boa.CodeMin`, `boa.CodeMax` | Value or length is out of bounds |
| `boa.CodePattern` | Pattern check failed |
| `boa.CodeCustom` | Custom validator failed |
| `boa.CodeSchema` | Invalid metadata or input shape |
| `boa.CodeLimit` | Traversal or copying exceeded a limit |

Both modes report the first failed rule per field; field-local depth and schema
errors do not suppress sibling errors.

| Limit | Purpose |
|---|---|
| 100 errors | Caps the diagnostic count; `Truncated` marks the cap |
| 128 recursive traversal or copy steps | Prevents stack growth, including pointer and interface dereferences |
| 10,000 structural traversal visits | Limits repeated visits to shared graphs |

Scalar leaves are not traversed, and scalar-only `[]any` values are skipped.
Cycles are skipped on the active branch; separate aliases are checked
independently. The visit limit does not bound copying, map key sorting,
alternatives scans, application callbacks, decoding, or I/O.

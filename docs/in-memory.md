# In-memory defaults and validation

Use `boa.Defaults[T]()` to construct a fresh struct and `boa.Validate(value)` to
validate a struct or non-nil pointer to one. They are independent operations:

```go
type Settings struct {
    Enabled bool   `json:"enabled" default:"true"`
    Port    int    `json:"port" default:"8080" min:"1" max:"65535"`
    Mode    string `json:"mode" default:"tcp" alts:"tcp,http"`
}

settings, err := boa.Defaults[Settings]()
if err != nil {
    return err
}
if err := boa.LoadConfigBytes(data, ".json", &settings, nil); err != nil {
    return err
}
return boa.Validate(settings)
```

For persisted, already normalized data, decode into a zero value and call only
`Validate`. `LoadConfigBytes` remains a decoder: it adds neither defaults nor
validation. Strict key/type/presence checks belong to the decoder or application.

## Presence and defaults

| In-memory value | Presence during validation |
|---|---|
| `0`, `false`, `""`, any other scalar | Present; rules run |
| Nil pointer, slice, map, or interface | Absent; required fails, optional skips rules |
| Non-nil pointer to a zero value | Present; rules run on its value |
| Non-nil empty slice or map | Present; length rules run |
| Value struct | Present; its fields are traversed |
| Nil pointer to a struct | Absent; its children are not traversed |

There is no way to distinguish an omitted scalar from an explicitly assigned
zero in an existing Go struct. Validation does not guess. A required string can
be empty; use `min:"1"` to require content. A required Boolean can be false.
Use pointers or decoder-specific presence checks when omission matters.

`Defaults` constructs a new value rather than filling zero fields in an existing
one. Decode onto it **after** construction: explicit `0`, `false`, `""`, and
pointer `null` replace defaults according to your decoder. Omitted keys retain
defaults. No new presence tracking is needed. Never use this flow to repair
missing persisted data implicitly.

Defaults use Boa's existing typed text parsers. Nested value structs receive
their defaults; nil pointer groups remain nil unless explicitly defaulted through
configuration. Collections declared with defaults are whole values; their element
fields do not receive an additional default overlay. To construct collection
elements before decoding them, call `Defaults[Element]()` in the owning loader.
Maps, slices, and pointers returned from defaults have independent storage,
including programmatically configured defaults.

There is no Boolean enricher in the standalone functions: an untagged bool is
simply Go's false. Required/optional tags and `WithDefaultOptional` retain their
existing metadata meanings. `boa:"ignore"` and unexported fields are skipped;
CLI input directives such as `configonly` do not disable in-memory validation.

## Shared rules and metadata

Both functions accept optional `boa.ConfigureField` callbacks. They receive an
exported Go field path and the existing `boa.Parameter`, after tag parsing:

```go
err := boa.Validate(settings, func(path string, p boa.Parameter) {
    if path == "Port" {
        p.SetMin(1024)
        field := boa.Field[int]{Parameter: p}
        field.SetCustomValidator(func(port int) error {
            if port == 9000 {
                return errors.New("port reserved by this application")
            }
            return nil
        })
    }
})
```

Reuse the same field configuration in a command's Init hook via
`boa.Param(ctx, &params.Port).Parameter`. Standalone functions do not execute
that hook automatically. Defaults callbacks may call `SetDefault`; validation
callbacks may change constraints, but `SetDefault` cannot make absent input
present during validation. Disabled and ignored fields skip rules and children.

The shared evaluators check required presence, strict alternatives, custom
validators, then min/max/pattern. Each field reports its first failed rule.
Collection alternatives check each slice element and report the first invalid
index. Min/max on collections constrain length, not each element's magnitude.
Structs inside slices, arrays, and string-keyed maps are traversed; registered
and text scalar types are opaque. Map keys are visited in sorted order.

`Validate` copies input before invoking custom validators, including reference
values. It does not apply defaults, normalize values, expand environment
variables, check files, resolve paths or secrets, enforce command HTTPBind policy,
or run lifecycle hooks. `Defaults` likewise performs no source lookup or path
resolution. Registered parsers, configuration callbacks, and validators are
application code; Boa cannot prevent their own external side effects.

## Structured errors

```go
var validation *boa.ValidationError
if errors.As(err, &validation) {
    for _, field := range validation.Fields {
        fmt.Println(field.Path, field.Code, field.Err)
    }
    // Truncated means traversal stopped at MaxValidationErrors (100).
}
```

Paths use exported Go names, including embedded fields: `Health.Readiness.Type`,
`Apps[2].HTTP.Port`, or `Groups["west"].Port`. They do not depend on flag names or
JSON/TOML tags. Applications can map these paths to their wire format.

Stable categories are `required`, `alternatives`, `min`, `max`, `pattern`,
`custom`, `schema` (invalid metadata/input shape), and `limit` (nesting beyond
128 levels). Compare `Code` with the `boa.Code*` constants. `FieldError` and
`ValidationError` support `errors.As`; custom causes support `errors.Is`.
Messages are diagnostics rather than a parsing contract. Collection stops at
100 failures; repeated references on an active cyclic branch are not traversed
again. Defaults stops at its first construction error.

## Implementation and compatibility audit

The change keeps the existing `paramMeta` and `newParam` representation. Tag
parsing for alternatives, strictness, typed defaults, bounds, and patterns is
extracted from `processingContext.applyTags` into `applyFieldTags`; validation
does not even parse default literals. Default copying uses the existing
`cloneConfigValue`, extended from slices to all reference defaults.

The required, alternatives, custom-validator, and min/max/pattern evaluators are
shared with `command.validate`. The command calls them against its live mirrors,
preserving constraints enriched during Init. Its adapter retains source-aware
required checks, conversion, environment hints, first-error messages, HTTPBind
policy, file checks, secret loading, precedence, lifecycle hooks, and reload.
CLI traversal is unchanged; standalone traversal additionally reaches struct
fields within collections. No new validation library or parallel metadata
representation is introduced.

The paired rule tests compare CLI error messages to standalone rule diagnostics,
including bounds beyond 2^53, unsigned bounds, duration, named strings,
collections, and Init-enriched/custom rules. Existing command integration tests
continue to cover precedence, hooks, files, secrets, HTTPBind, and reload.

## Consumer assessment

The runnable [consumer example](https://github.com/j0sh/boa/tree/main/internal/example_memory)
uses representative fields inspected in Clankery `26c84a9` and Node `23c1f6a`.
It is a subset demonstration, not a migration or a replacement for either
application's full validator. Its tests compare the sample's six basic rules
and defaults with an explicit manual baseline.

| Consumer | Checks/defaults Boa can supply | Checks that stay with the consumer |
|---|---|---|
| Clankery `schema` | Readiness defaults and type enum; failure threshold 1–10; HTTP port 1–65535; instance mode enum, replicas 1–100, capacity 1–10000; other literal defaults/enums can use tags | Strict TOML kinds/keys/presence, conditional readiness default when a table is supplied, normalized JSON completeness, canonical bytes/digest, exact quantity/price parsing, image references, path safety/overlap, resource sums, timeout relationships, single-session capacity, payment policy |
| Node `signer.KafkaConfig` | Existing auth enum/default, outbox path default, outbox capacity default and positive bound | Broker address/scheme/port rules; topic syntax and special names; outbox URI/path restrictions; credential pairing, secret-file resolution and NUL checks; SASL transport construction |
| Node orchestrator `validateApp` | Capacity 1–10000, nonempty app, mode enum, optional route ID length/pattern, GPU VRAM bound; fresh request defaults for capacity/mode/status | Explicit zero/empty compatibility normalization, lowercasing/trimming price metadata, destination URL checks, exact pricing, ownership, duplicate IDs, admission and cross-field rules |

For Kafka, the existing explicit `OutboxMaxBytes <= 0` branch is redundant once
`boa.Validate` runs. Topic length/syntax would need **new tags** before those
checks could move. `transport()` currently permits empty auth as SCRAM fallback,
while its existing strict Boa enum rejects empty: a consumer migration must
decide whether to retain that compatibility exception. Do not silently replace
transport construction with enum checking.

Node registration currently converts explicit capacity `0` to `1`, and explicit
empty mode/status to defaults. `Defaults` before decoding only handles omission;
`Validate` preserves explicit zero/empty. Retain those normalization branches or
deliberately version that contract. Status currently accepts arbitrary nonempty
strings, so adding a status enum would also change behavior.

No consumer files are changed. Savings are measured on the runnable subset,
not inferred from the earlier 10–30 schema LOC estimate; they do not include
application rules that still have to run.

The sample's two manual helpers occupy 28 physical Go lines; the two helpers
using Boa occupy 7, a reduction of **21 lines** with identical tested basic rule
results. These counts include the separating blank line and function boundaries.
Tags replace existing field declarations without adding lines. The loader needs
3 extra lines to handle a possible default-construction error, making the net
sample reduction **18 lines** when that call site is included. The manual
baseline stays in the example's test file solely to verify the comparison.

For the Boa change itself (physical Go lines, including comments and blanks,
excluding pre-existing editor-swap deletions):

| Area | Added | Removed | Net |
|---|---:|---:|---:|
| Framework production | 469 | 140 | +329 |
| Runnable consumer example | 70 | 0 | +70 |
| Tests, including the manual consumer baseline | 494 | 0 | +494 |

The production totals include **108 original lines relocated with edits**:
68 in the bounds/pattern evaluator and 40 in field-tag parsing. These are moves,
not 108 additional lines of new rule logic; alternatives/custom/required
extraction is a refactor rather than an unchanged move. Counts use
`git diff --numstat` against the parent plus full line counts for new Go files;
the moved-block counts measure their original source spans separately.

# BOA Agent Guide

BOA is a Go 1.27 declarative CLI framework built on Cobra. Its primary API is `boa.Cmd[T]`.

## Repository map

- `pkg/boa/api*.go`: public command and parameter APIs.
- `pkg/boa/internal.go`, `pipeline.go`, `tags.go`, `param_meta.go`: traversal, sourcing, validation, and metadata.
- `pkg/boa/type_handler.go`, `config_exact_types.go`, `text.go`: value parsing and custom types.
- `pkg/boa/reload.go`: live reload.
- `pkg/boaviper/`: optional config discovery.
- `internal/`: runnable examples and integration fixtures.
- `docs/` and `README.md`: user-facing behavior and examples.

Use `rg` to locate the current implementation before editing; responsibilities may move during refactors.

## Invariants

- Value precedence is CLI > env > root config > nested config > default > zero value.
- Metadata changed programmatically must be set during `InitFunc` or `InitFuncCtx`, before flag binding and env parsing.
- Optional struct pointers are temporarily allocated for traversal, then restored to nil unless explicitly populated; defaults alone do not keep them alive.
- `boa:"ignore"` removes a field from BOA processing. `boa:"configonly"` preserves its mirror and validation while disabling CLI and env input.
- Config formats normally dispatch by file extension. Per-command format fields bypass that registry.

Treat tests and public documentation as the specification. Add a regression test for behavioral changes and update relevant docs or examples with public API changes.

## Verification

Format changed Go files, then run the narrowest relevant test followed by:

```bash
go test ./...
go vet ./...
go test -race ./...
```

For config-decoder changes, also run `go test -race ./...` from `integration/formats/`.

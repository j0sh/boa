# BOA Agent Guide

BOA is a Golang declarative CLI framework built on Cobra. Its primary API is `boa.Cmd[T]`.

Use the Go version declared in `go.mod`.

## Repository map

- `pkg/boa/`: core framework, public APIs, and unit tests.
- `pkg/boaviper/`: optional config discovery.
- `internal/`: runnable examples and integration fixtures.
- `integration/formats/`: decoder compatibility tests in a separate Go module; root `./...` commands do not include it.
- `docs/` and `README.md`: user-facing behavior and examples.
- `.github/workflows/`: CI checks, documentation builds, and releases.

Use `rg` to locate the relevant implementation and tests before editing.

## Making changes

Read the relevant tests and documentation before changing behavior. [docs/index.md](docs/index.md) summarizes the core contracts and links to the detailed references.

Add a regression test for behavioral changes. Update relevant docs and runnable examples when public APIs or documented behavior change.

## Verification

For Go changes, run these checks from the repository root:

```bash
gofmt
golangci-lint run ./...
go vet ./...
go test ./...
go test -race ./...
```

For documentation-only changes, check links and formatting. If changing runnable examples, test the affected packages under `internal/`.

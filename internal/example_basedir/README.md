# Base directory example

From the repository root:

```sh
go run ./internal/example_basedir --data-dir ./internal/example_basedir/testdata
```

The default `config.json` and the config's `input.txt` resolve against `--data-dir`. The command prints the resolved paths. `StateFile` declares its default with a tag:

```go
StateFile string `basepath:"basedir" default:"state.db"`
```

It resolves to `state.db` under `basedir`. The example creates `basedir` when needed and prints paths without creating the state file.

## Derive a directory from merged config

Node services keep mainnet and testnet state separate. `PostConfigFuncCtx` uses the merged `Network` value to derive `data/<network>` when `HasInput(&p.DataDir)` is false. A static tag cannot calculate a default from another setting.

For example, `config.json` in the working directory can contain:

```json
{"Network":"testnet"}
```

Run it from the repository root:

```sh
go run ./internal/example_basedir
```

This creates `<working-directory>/data/testnet` and resolves the state path to `data/testnet/state.db`. `--network mainnet` or `APP_NETWORK=mainnet` overrides the config. With no network input, the default is mainnet.

BOA loads `config.json` using the `DataDir` default of `.` before the hook runs. Changing `basedir` in the hook does not relocate or reload that config. An explicit `DataDir` from CLI, environment, or config is preserved, including `.` or an empty string. `APP_DATA_DIR` supplies the same setting as `--data-dir`.

## Explicit directories and source-relative files

To keep data in `data/` and load an existing `runner.json` beside the config, put this in `config.json` in the working directory:

```json
{"DataDir":"data","RunnerFile":"runner.json"}
```

`RunnerFile` uses `basepath:"source"`: paths from config are relative to the config file; CLI/environment paths are relative to the original working directory.

A missing default config is skipped. To create a new empty directory:

```sh
go run ./internal/example_basedir --data-dir ./new-data
```

See [Base directories](../../docs/struct-tags.md#base-directories) for tag options.

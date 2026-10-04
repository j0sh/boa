# Base directory example

From the repository root:

```sh
go run ./internal/example_basedir --data-dir ./internal/example_basedir/testdata
```

The default `config.json` and the config's `input.txt` resolve against `--data-dir`. The command prints the resolved absolute paths. `APP_DATA_DIR` supplies the same setting when the flag is absent.

A missing default config is skipped. To create a new empty directory:

```sh
go run ./internal/example_basedir --data-dir ./new-data
```

See [Base directories](../../docs/struct-tags.md#base-directories) for tag options.

# Base directory example

From the repository root:

```sh
go run ./internal/example_basedir --data-dir ./internal/example_basedir/testdata
```

The default `config.json` and the config's `input.txt` resolve against `--data-dir`. The command prints the resolved absolute paths. `APP_DATA_DIR` supplies the same setting when the flag is absent.

To create a new empty directory without loading a config:

```sh
go run ./internal/example_basedir --data-dir ./new-data --config-file=
```

See [Base directories](../../docs/struct-tags.md#base-directories) for tag options.

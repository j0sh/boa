# Live Config Reload

`boa.Reload[T](ctx)` rebuilds one command's parameters, replays its saved invocation, reloads sources, and validates a fresh `*T`. It never mutates the parameter struct currently used by the application.

## Minimal pattern

Publish successful snapshots with `atomic.Pointer`:

```go
var active atomic.Pointer[Params]

boa.Cmd[Params]{
    Use: "server",
    RunFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command, _ []string) {
        active.Store(p)

        go func() {
            signals := make(chan os.Signal, 1)
            signal.Notify(signals, syscall.SIGHUP)
            for range signals {
                fresh, err := boa.Reload[Params](ctx)
                if err != nil {
                    log.Printf("reload rejected: %v", err)
                    continue
                }
                active.Store(fresh)
            }
        }()

        serve(&active)
    },
}.Run()
```

Readers call `active.Load()` and always receive a complete old or new snapshot.

## Replay semantics

Reload builds fresh parameters from the saved CLI flags and positional arguments, rereads environment and config, and returns the new pointer after validation succeeds. It uses the original working directory and any `basedir` inherited from the parent. It recalculates this command's `basedir` before loading config; the value from the previous config load is not reused. Reload never creates directories.

Calls on the same `HookContext` are serialized. Reload affects only that command and does not rerun children. On error it returns `(nil, err)` and leaves the previous parameters and watch list intact. Hook side effects cannot be rolled back; application panics are not recovered.

## Hook behavior on reload

Reload runs Init, PostCreate, PreConfig, PostConfig, PreValidate, and field validation. Keep these hooks repeatable. PreExecute and Run are skipped; use them for one-time resource startup. See [execution order](lifecycle.md#execution-order).

## Watched files

`ctx.WatchedConfigFiles()` returns the config files successfully loaded, excluding missing optional files. A successful reload refreshes the list using the same [missing-file policy](configuration.md#automatic-loading).

Files loaded manually with `LoadConfigFile`, `LoadConfigFiles`, or `LoadConfigBytes` are outside the automatic pipeline. Register filesystem paths explicitly in a context-aware PreValidate hook:

```go
PreValidateFuncCtx: func(ctx *boa.HookContext, p *Params, _ *cobra.Command, _ []string) error {
    const path = "/etc/myapp/overrides.json"
    if err := boa.LoadConfigFile(path, p, nil); err != nil {
        return err
    }
    ctx.WatchConfigFile(path)
    return nil
},
```

## Choosing a trigger

BOA deliberately does not include a filesystem watcher. Call `Reload` from the trigger that fits the application:

- SIGHUP for a POSIX service;
- an authenticated administrative endpoint;
- a timer or control-plane event;
- a filesystem watcher.

When using fsnotify-style libraries, watch the containing directories rather than only the files so atomic rename-based saves continue to work. Reconcile the watched set after each successful reload because config paths can themselves change.

## Multiple commands

Each executed command receives its own `HookContext` and saved invocation. Reload the context whose parameter type you want to refresh. Inherited persistent flags remain owned by the command that declared them; a parent and leaf with independent live configurations should each publish and reload their own snapshots.

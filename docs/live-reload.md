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

A reload:

1. allocates a new parameter struct;
2. rebuilds that command's mirrors and bindings;
3. reruns Init and PostCreate hooks;
4. restores the parsed CLI flags and positional arguments from the actual invocation;
5. reloads environment variables and config files with normal precedence;
6. reruns PreValidate hooks and field validation;
7. returns the fresh pointer only after all steps succeed.

The saved invocation is independent of later changes to `os.Args`. Reload does not route through child commands or reparent the original Cobra tree. Calls through the same `HookContext` are serialized.

On a returned error, `Reload` returns `(nil, err)` and the caller should keep the previous snapshot. Setup and PreValidate hooks may affect external systems; those effects cannot be rolled back. Panics from application hooks or invalid API use are not recovered.

## Hook behavior on reload

| Phase | Runs? |
|---|---:|
| Struct and command Init | yes |
| Struct and command PostCreate | yes |
| Environment/config sourcing | yes |
| Struct and command PreValidate | yes |
| Field validation | yes |
| Struct and command PreExecute | no |
| Run functions | no |

Keep Init, PostCreate, and PreValidate repeatable. Put one-time resource startup in PreExecute or Run.

## Watched files

`ctx.WatchedConfigFiles()` returns files loaded through `configfile:"true"`, including nested paths, overlay chains, registered formats, and a command-level format override. A successful reload refreshes this list; a failed reload leaves the previous list intact.

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

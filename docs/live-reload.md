# Live Config Reload

Long-running programs — servers, daemons, background workers — often want to re-read config without restarting. BOA ships a primitive for this: `boa.Reload[T](ctx) (*T, error)`. It allocates a fresh `*T`, rebuilds that command's parameter bindings, restores its parsed CLI values, and runs source loading and validation.

## Quick Start

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "sync/atomic"
    "syscall"

    "github.com/GiGurra/boa/pkg/boa"
    "github.com/spf13/cobra"
)

type Params struct {
    ConfigFile string `configfile:"true" optional:"true"`
    Host       string `optional:"true"`
    Port       int    `optional:"true" default:"8080"`
}

var active atomic.Pointer[Params]

func main() {
    boa.Cmd[Params]{
        Use: "server",
        RunFuncCtx: func(ctx *boa.HookContext, p *Params, cmd *cobra.Command, args []string) {
            active.Store(p)

            // Wire a trigger — here, SIGHUP.
            sighup := make(chan os.Signal, 1)
            signal.Notify(sighup, syscall.SIGHUP)
            go func() {
                for range sighup {
                    fresh, err := boa.Reload[Params](ctx)
                    if err != nil {
                        log.Printf("config reload rejected: %v", err) // old state preserved
                        continue
                    }
                    active.Store(fresh)
                    log.Println("config reloaded")
                }
            }()

            startServer()
        },
    }.Run()
}
```

`kill -HUP <pid>` now re-reads every file BOA loaded at startup, re-applies precedence (CLI still wins), re-validates, and hands you a fresh `*Params` to atomically swap. A reader goroutine that does `cfg := active.Load()` always sees a consistent snapshot.

## What Reload does

1. **Allocates a fresh `*T`.** The struct you were handed in `RunFunc` is **not mutated**. Callers decide what to do with the new snapshot: atomic pointer swap, diff for "did the field I care about actually change?", notify subscribers, or discard entirely. BOA doesn't dictate a concurrency model.
2. **Re-runs setup and validation.** Init and PostCreate hooks run again, followed by restoring the original CLI values, env/config loading, PreValidate hooks, and field validation. CLI values retain precedence over env, config, and defaults.
3. **Skips every PreExecute and Run hook**, including struct methods. Reload does not route to subcommands or reparent the original Cobra tree.

## Error Handling: Reload is All-or-Nothing

For a returned load or validation error, `Reload` returns `(nil, err)` and does not publish a new parameter pointer. Keep the previous pointer until a reload succeeds. Setup and PreValidate hooks can still change external state; those effects are not rolled back. Panics from hooks or API misuse are not recovered.

| Failure | What the caller sees |
|---|---|
| **File parse error** (malformed JSON/YAML/TOML, truncated mid-write) | Error names the offending file. The fresh struct is discarded; the caller keeps the previous snapshot. |
| **Validation failure** (`min` / `max` / `pattern` / custom validator) | Error describes which field failed. Fresh struct is discarded before it ever leaves Reload. |
| **File disappeared** | Clean read error naming the path. |
| **PreValidate hook error** | Returned with hook context; the original error remains available through error unwrapping. |

A file watcher can emit multiple events for one save. Log rejected reloads and keep serving the existing snapshot:

```go
for range fileChanges {
    fresh, err := boa.Reload[Params](ctx)
    if err != nil {
        log.Printf("reload failed (keeping current config): %v", err)
        continue
    }
    active.Store(fresh)
}
```

## What Reload does NOT do

- **No built-in trigger.** Supply a signal handler, timer, admin endpoint, or file watcher.
- **No snapshot publication.** Calls through the same HookContext are serialized. Coordinate hooks that share state across different commands. Publish accepted snapshots with `atomic.Pointer[T]` or a mutex, and treat published snapshots as immutable.
- **No partial reload of a file chain.** Source loading and validation run against the full input set.

## Which Files Get Watched?

`ctx.WatchedConfigFiles()` returns the paths a live-reload watcher should listen on. Use this to hand the file set to fsnotify / your custom watcher of choice.

### Auto-tracked

- Files loaded through a `configfile:"true"` field (single path or `[]string` overlay chain), including loads using a per-command `ConfigFormat` override.

### Not auto-tracked

`boa.LoadConfigFile` / `LoadConfigFiles` / `LoadConfigBytes` called from inside a user hook — these are public helpers outside BOA's internal pipeline. Register those explicitly with `ctx.WatchConfigFile(path)` inside the same hook. The hook re-registers the path in each replay's new context. A successful reload refreshes the original context's watched-file list. A failed reload leaves that list unchanged:

```go
PreValidateFuncCtx: func(ctx *boa.HookContext, p *Params, cmd *cobra.Command, args []string) error {
    if err := boa.LoadConfigFile("/etc/myapp/overrides.json", p, nil); err != nil {
        return err
    }
    ctx.WatchConfigFile("/etc/myapp/overrides.json") // opt in to watching
    return nil
},
```

## Hook Behavior on Reload

| Hook | Runs on reload? |
|---|---|
| `InitFunc` / `InitFuncCtx` | ✅ |
| `PostCreateFunc` / `PostCreateFuncCtx` | ✅ |
| `PreValidateFunc` / `PreValidateFuncCtx` | ✅ |
| `PreExecuteFunc` / `PreExecuteFuncCtx` | ❌ (no main action to run) |
| `RunFunc` / `RunFuncCtx` / `RunFuncE` / `RunFuncCtxE` | ❌ (no main action to run) |
| `CfgStructInit` / `CfgStructPostCreate` / `CfgStructPreValidate` and Ctx variants | ✅ |
| `CfgStructPreExecute` / `CfgStructPreExecuteCtx` | ❌ |

Separate one-time application setup from configuration hooks. Each reload receives a fresh struct, so a sentinel stored in that struct does not survive replay.

Reload captures parsed flags and positional arguments from the actual invocation, including leaf commands reached through a parent. Changes to `os.Args` do not affect replay. Inherited flags belong to the declaring command's parameters; reload each command's context when both need refreshing. Setup hooks receive the isolated command being rebuilt.

## Typical Triggers

### SIGHUP (POSIX convention)

```go
sighup := make(chan os.Signal, 1)
signal.Notify(sighup, syscall.SIGHUP)
go func() {
    for range sighup {
        if fresh, err := boa.Reload[Params](ctx); err == nil {
            active.Store(fresh)
        }
    }
}()
```

### Admin HTTP endpoint

```go
http.HandleFunc("/admin/reload", func(w http.ResponseWriter, r *http.Request) {
    fresh, err := boa.Reload[Params](ctx)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    active.Store(fresh)
    w.WriteHeader(http.StatusNoContent)
})
```

### fsnotify (watch the directory, not the file, to survive atomic-rename-saves)

```go
watcher, _ := fsnotify.NewWatcher()
defer watcher.Close()
watched := ctx.WatchedConfigFiles()
dirs := map[string]bool{}
for _, p := range watched {
    dirs[filepath.Dir(p)] = true
}
for d := range dirs {
    watcher.Add(d)
}
targets := map[string]bool{}
for _, p := range watched {
    targets[p] = true
}
debounce := time.NewTimer(time.Hour)
debounce.Stop()
for {
    select {
    case ev := <-watcher.Events:
        if targets[ev.Name] {
            debounce.Reset(200 * time.Millisecond)
        }
    case <-debounce.C:
        if fresh, err := boa.Reload[Params](ctx); err == nil {
            active.Store(fresh)
        }
    }
}
```

### Timer (poll every N seconds)

```go
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()
for range ticker.C {
    if fresh, err := boa.Reload[Params](ctx); err == nil {
        active.Store(fresh)
    }
}
```

## Reading the Active Config from Worker Goroutines

The atomic-pointer pattern keeps readers lock-free and always-consistent:

```go
var active atomic.Pointer[Params]

func handleRequest(w http.ResponseWriter, r *http.Request) {
    cfg := active.Load() // always points at a fully-validated, immutable snapshot
    fmt.Fprintf(w, "host=%s port=%d\n", cfg.Host, cfg.Port)
}
```

Each `active.Load()` returns the pointer that was current when the load began. A concurrent `Store` from the reload goroutine can swap it — in-flight readers continue with the old snapshot, new readers see the new one. No torn reads, no locks.

If you need to react to specific changes — "port changed, restart the listener" — diff the old and new snapshots after the swap:

```go
old := active.Load()
fresh, err := boa.Reload[Params](ctx)
if err != nil {
    return
}
active.Store(fresh)
if fresh.Port != old.Port {
    go restartListener(fresh.Port)
}
```

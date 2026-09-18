# Cobra Interoperability

BOA builds ordinary `*cobra.Command` values. It exposes Cobra types in its command definition and passes the active command to hooks and run functions, so a command tree can mix BOA and native Cobra at any depth.

## Cobra fields on Cmd

`boa.Cmd[T]` accepts Cobra's own types where Cobra already has the right abstraction:

```go
boa.Cmd[Params]{
    Use:     "serve [extra...]",
    Aliases: []string{"server"},
    Args:    cobra.MaximumNArgs(2),
    Groups:  []*cobra.Group{{ID: "core", Title: "Core commands:"}},
    SubCmds: []*cobra.Command{existingCommand},
}
```

Relevant fields include `Args`, `Groups`, `GroupID`, `SubCmds`, `Aliases`, `ValidArgs`, and `ValidArgsFunc`.

## Accessing the active command

Every command function receives `*cobra.Command`:

```go
boa.Cmd[Params]{
    Use: "app",
    InitFunc: func(_ *Params, cmd *cobra.Command) error {
        cmd.Deprecated = "use new-app instead"
        cmd.SilenceUsage = true
        return nil
    },
    RunFunc: func(p *Params, cmd *cobra.Command, args []string) {
        cmd.Printf("running %s with %d args\n", cmd.Name(), len(args))
    },
}
```

Use Init for command properties and PostCreate for operations that need BOA's generated flags to exist.

## Converting a BOA command

`ToCobra` builds and returns the underlying command:

```go
cmd := boa.Cmd[Params]{
    Use:    "serve",
    RunFunc: runServe,
}.ToCobra()

cmd.SetHelpTemplate(customHelp)
root.AddCommand(cmd)
```

`ToCobraE` returns construction errors instead of panicking. See [Lifecycle and Errors](lifecycle.md#tocobra-and-tocobrae).

## Mixing command trees

### Native Cobra parent

Add a BOA child like any other Cobra command:

```go
root := &cobra.Command{Use: "tool"}
root.AddCommand(
    legacyCommand,
    boa.Cmd[ServeParams]{
        Use:    "serve",
        RunFunc: runServe,
    }.ToCobra(),
)
```

### BOA parent

`SubCmds` is `[]*cobra.Command`, so native children fit directly. `boa.SubCmds` converts a heterogeneous list of typed BOA commands:

```go
root := boa.Cmd[boa.NoParams]{
    Use: "tool",
    SubCmds: append(
        boa.SubCmds(
            boa.Cmd[ServeParams]{Use: "serve", RunFunc: runServe},
            boa.Cmd[DeployParams]{Use: "deploy", RunFunc: runDeploy},
        ),
        legacyCommand,
    ),
}
root.Run()
```

This is also the incremental adoption strategy: convert one leaf command at a time, then convert parents only when useful.

## Cobra argument validation

BOA derives positional counts from `positional:"true"` fields. Set `Args` when Cobra's validators better express the rule:

```go
boa.Cmd[Params]{
    Use: "greet [names...]",
    Args: cobra.MinimumNArgs(1),
    RunFunc: func(_ *Params, _ *cobra.Command, names []string) {
        for _, name := range names {
            fmt.Println("Hello", name)
        }
    },
}.Run()
```

BOA wraps argument-validator failures as user input errors.

## Command groups

```go
boa.Cmd[boa.NoParams]{
    Use: "tool",
    Groups: []*cobra.Group{
        {ID: "core", Title: "Core commands:"},
        {ID: "admin", Title: "Administrative commands:"},
    },
    SubCmds: boa.SubCmds(
        boa.Cmd[RunParams]{Use: "run", GroupID: "core"},
        boa.Cmd[UserParams]{Use: "users", GroupID: "admin"},
    ),
}
```

When a child has a `GroupID` not listed in `Groups`, BOA creates a group for it automatically.

## Flag relationships

Cobra's generated-flag relationship APIs work in PostCreate:

```go
PostCreateFunc: func(_ *Params, cmd *cobra.Command) error {
    cmd.MarkFlagsMutuallyExclusive("json", "yaml")
    cmd.MarkFlagsRequiredTogether("host", "port")
    return nil
},
```

Other Cobra facilities—completion commands, help templates, output buffers, documentation generation, and ecosystem packages—operate on the result of `ToCobra` without BOA-specific adapters.

## Executing an assembled tree

Use Cobra's `Execute` when the surrounding application owns output and error policy. Use `boa.Execute(root)` to print usage and errors with BOA's default command-line behavior.

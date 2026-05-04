# cmd/ Conventions

Conventions for adding or modifying Cobra commands under `cmd/`. The goal
is uniformity so reviewers don't have to re-learn local style and so users
get predictable behaviour for things like typo'd subcommand names.

## 1. Every leaf command must declare `Args:`

A "leaf" command is any `cobra.Command` that defines `RunE` (or `Run`).
Cobra's default is `ArbitraryArgs`, which silently accepts anything —
including typos like `sol writ creat <args>`, where the typo gets mapped
to the parent and the args fall on the floor.

Always declare an explicit `Args:` constraint:

```go
var fooCmd = &cobra.Command{
    Use:          "foo",
    Args:         cobra.NoArgs,         // takes no positional args
    SilenceUsage: true,
    RunE:         func(...) error { ... },
}

var fooBarCmd = &cobra.Command{
    Use:          "bar <id>",
    Args:         cobra.ExactArgs(1),   // takes exactly one
    SilenceUsage: true,
    RunE:         func(...) error { ... },
}
```

Common Cobra constraints:

- `cobra.NoArgs` — most commands; flag-driven invocation.
- `cobra.ExactArgs(N)` — fixed-arity positional commands.
- `cobra.MinimumNArgs(N)`, `cobra.MaximumNArgs(N)`, `cobra.RangeArgs(min, max)` —
  variable-arity commands.

Parent commands that only group subcommands and have no `RunE` of their
own do not need an `Args:` declaration; Cobra dispatches to subcommands
or prints help.

## 2. Flag bindings: pick one form per command, stick with it

Cobra offers two binding styles:

- **Typed `*Var` form** (preferred when the flag is read in multiple
  places or branches the command's behaviour):

  ```go
  var fooJSON bool
  fooCmd.Flags().BoolVar(&fooJSON, "json", false, "output as JSON")
  // ...
  if fooJSON { ... }
  ```

- **Anonymous form with inline `Get*` accessors** (acceptable when the
  flag is read once at the top of `RunE`):

  ```go
  fooCmd.Flags().Bool("json", false, "output as JSON")
  // ...
  asJSON, _ := cmd.Flags().GetBool("json")
  ```

Within a single command, **don't mix the two forms**. Either every flag
on the command is typed, or every flag is anonymous-with-`Get*`. Mixing
forces readers to track two patterns and tends to produce silently-broken
flags when refactors don't update both ends.

When in doubt, prefer typed `*Var` — the compiler catches typos that
`GetBool("jsno")` won't.

## 3. `MarkFlagRequired` (and friends): discard the error explicitly

`cmd.MarkFlagRequired(name)` returns an error only if the named flag was
never registered — a programming bug, not a runtime condition. Always
discard the error explicitly so linters don't complain and the intent is
obvious:

```go
fooCmd.Flags().StringVar(&fooName, "name", "", "name (required)")
_ = fooCmd.MarkFlagRequired("name")
```

The same applies to other registration-time helpers like
`MarkDeprecated` and `MarkFlagFilename`.

## 4. `SilenceUsage: true` on commands with `RunE`

Commands that return errors from `RunE` should set `SilenceUsage: true`
so a runtime failure doesn't dump the full usage banner. Cobra's default
is to print usage on any non-nil return, which buries the actual error.

## 5. Command groups

Use the package-level `groupID` constants (`groupSetup`, `groupAgents`,
`groupWrits`, `groupProcesses`, `groupPlumbing`) on top-level commands.
Subcommands inherit grouping from their parent and don't need to set
`GroupID` directly.

## 6. Hidden plumbing commands

Mark internal/plumbing commands `Hidden: true` and assign them
`GroupID: groupPlumbing`. They still appear in `--help --hidden` but
don't clutter the default help output.

# Prompt 03: Arc 3.5 — Governor Mirror Elimination and World Sync

**Working directory:** ~/gt-src/
**Prerequisite:** Arc 3.5 prompt 02 complete, `make build && make test` passing

## Context

Read `CLAUDE.md` for project conventions.
Read `docs/decisions/0014-managed-world-repository.md` for the ADR.
Read `internal/governor/governor.go` for `SetupMirror`, `RefreshMirror`, `MirrorPath`,
`GovernorDir`, and `Start`.
Read `internal/protocol/claudemd.go` for `GenerateGovernorClaudeMD` and
`GovernorClaudeMDContext`.
Read `cmd/governor.go` for the `refresh-mirror` subcommand and governor start flow.
Read `cmd/world.go` for existing world subcommands.
Read `internal/config/config.go` for `RepoPath()`.

## Task 1: Add `sol world sync` Command

In `cmd/world.go`, add a new subcommand:

```go
var worldSyncWorld string

var worldSyncCmd = &cobra.Command{
    Use:          "sync <name>",
    Short:        "Sync the managed repo with its remote",
    Long:         `Fetch and pull latest changes from the source repo's origin.
If the managed repo doesn't exist yet but source_repo is configured
in world.toml, clones it first.`,
    Args:         cobra.ExactArgs(1),
    SilenceUsage: true,
    RunE: func(cmd *cobra.Command, args []string) error {
        name := args[0]

        if err := config.RequireWorld(name); err != nil {
            return err
        }

        repoPath := config.RepoPath(name)

        // If managed repo doesn't exist, try to clone from config.
        if _, err := os.Stat(repoPath); os.IsNotExist(err) {
            worldCfg, err := config.LoadWorldConfig(name)
            if err != nil {
                return err
            }
            if worldCfg.World.SourceRepo == "" {
                return fmt.Errorf("no managed repo and no source_repo configured for world %q", name)
            }
            fmt.Printf("Cloning %s into managed repo...\n", worldCfg.World.SourceRepo)
            if err := setup.CloneRepo(name, worldCfg.World.SourceRepo); err != nil {
                return err
            }
            fmt.Printf("Managed repo created for world %q\n", name)
            return nil
        }

        // Fetch from origin.
        fetchCmd := exec.Command("git", "-C", repoPath, "fetch", "origin")
        if out, err := fetchCmd.CombinedOutput(); err != nil {
            return fmt.Errorf("failed to fetch for world %q: %s: %w",
                name, strings.TrimSpace(string(out)), err)
        }

        // Pull with fast-forward only.
        pullCmd := exec.Command("git", "-C", repoPath, "pull", "--ff-only")
        if out, err := pullCmd.CombinedOutput(); err != nil {
            return fmt.Errorf("failed to pull for world %q: %s: %w",
                name, strings.TrimSpace(string(out)), err)
        }

        fmt.Printf("Synced managed repo for world %q\n", name)
        return nil
    },
}
```

Register in `init()`:
```go
worldCmd.AddCommand(worldSyncCmd)
```

Import `"os/exec"`, `"strings"`, and `"github.com/nevinsm/sol/internal/setup"`
in world.go (setup may already be imported after prompt 02).

## Task 2: Remove Governor Mirror

### Remove `MirrorPath` and mirror functions from `governor.go`

In `internal/governor/governor.go`:

1. **Remove `MirrorPath` function** (returns `$SOL_HOME/{world}/governor/mirror/`).

2. **Remove `SetupMirror` function** entirely. This was the governor's private
   clone logic — replaced by the managed repo at `config.RepoPath(world)`.

3. **Remove `RefreshMirror` function** entirely. Replaced by `sol world sync`.

4. **Update `Start`**: Remove the `SetupMirror` call and the `SourceRepo` field
   from `StartOpts`.

   In `StartOpts`, remove:
   ```go
   SourceRepo   string
   ```

   In `Start()`, remove the `SetupMirror` call (the lines that call
   `SetupMirror(opts.World, opts.SourceRepo, opts.TargetBranch)` and the
   stderr warning).

   Also remove `TargetBranch` from `StartOpts` if it was only used by
   `SetupMirror`. Check if `Start` uses it elsewhere — if not, remove it.

### Update `cmd/governor.go`

1. **Remove `governorRefreshMirrorCmd`** entirely — the command, its variable
   (`governorRefreshMirrorWorld`), its flag registration, and its `AddCommand`.

2. **Update `governorStartCmd`**: Remove the `sourceRepo` resolution and
   `SourceRepo` from `StartOpts`:

   ```go
   // Remove these lines:
   sourceRepo := ...
   if sourceRepo == "" { ... }
   if sourceRepo == "" { return error }
   ```

   Remove `SourceRepo` and `TargetBranch` from the `governor.StartOpts{}` struct
   literal (if `TargetBranch` was removed from `StartOpts`).

   The CLAUDE.md installation still needs the mirror directory. Change
   `MirrorDir` in the `GovernorClaudeMDContext` from `"mirror"` to the path
   of the managed repo relative to `govDir`. Since `govDir` is
   `$SOL_HOME/{world}/governor/` and the repo is at `$SOL_HOME/{world}/repo/`,
   the relative path is `"../repo"`:

   ```go
   if err := protocol.InstallGovernorClaudeMD(govDir, protocol.GovernorClaudeMDContext{
       World:     governorStartWorld,
       SolBinary: "sol",
       MirrorDir: "../repo",
   }); err != nil {
   ```

3. **Remove `--source-repo` flag** from governor start if it wasn't already
   removed in prompt 02. Verify it's gone.

### Update governor hooks

In `internal/governor/governor.go`, the `installHooks` function has a
SessionStart hook that calls `sol governor refresh-mirror --world=...`. Change
it to call `sol world sync`:

Find the hook entry with `refresh-mirror` and change to:

```go
{
    Type:    "command",
    Matcher: "SessionStart",
    Command: fmt.Sprintf("sol world sync %s", world),
},
```

Also update the compact hook if it references mirror refresh.

## Task 3: Update Governor CLAUDE.md Template

In `internal/protocol/claudemd.go`, `GenerateGovernorClaudeMD`:

### Codebase Research section

The current template says:
```
## Codebase Research
- Read-only mirror at `%s/` — use for understanding code, never edit
- Pull latest before major research: `git -C %s pull --ff-only`
```

Change to:
```
## Codebase Research
- Read-only codebase at `%s/` — use for understanding code, never edit
- Sync latest before major research: `sol world sync %s`
```

The second `%s` changes from `ctx.MirrorDir` to `ctx.World`. Update the
Sprintf arguments accordingly.

Verify all `Sprintf` arguments still align after the change. Count the format
verbs and match them to the argument list.

### Tests

In `internal/protocol/claudemd_test.go`, `TestGenerateGovernorClaudeMD`:

- Remove assertions that check for `mirror/` (the old mirror directory reference)
- Add assertions for `../repo/` or whatever the new MirrorDir value is
- Add assertion for `sol world sync` command in the output
- Remove assertion for `git -C mirror pull --ff-only`

In `internal/governor/governor_test.go`:

- Remove `TestSetupMirror`, `TestSetupMirrorRefresh`, `TestRefreshMirror`,
  `TestRefreshMirrorNonMainBranch`, `TestSetupMirrorCorruptedDirectory`, and
  any other mirror-related tests
- Update `TestStart` to not set `SourceRepo` or `TargetBranch` in `StartOpts`
- Update hook assertions to check for `sol world sync` instead of
  `sol governor refresh-mirror`

In `cmd/governor_test.go`:

- Remove tests for the `refresh-mirror` subcommand if any exist

## Task 4: Update Integration Tests

In `test/integration/arc3_test.go`:

### Governor start tests

Governor start tests that pass `--source-repo=` should have had this flag
removed in prompt 02. Verify. If any remain, remove them.

### `TestGovernorRefreshMirror`

This test exercises the old `sol governor refresh-mirror` command. Replace it
with a test for `sol world sync`:

- Rename to `TestWorldSync`
- Change the command from `governor refresh-mirror --world=myworld` to
  `world sync myworld`
- The test should verify that after sync, the managed repo at
  `config.RepoPath("myworld")` has the latest commits from the source

### `TestGovernorHooksInstalled`

Update the hook verification to look for `sol world sync` instead of
`sol governor refresh-mirror`.

### Add `TestWorldSyncCreatesClone`

Test the late-initialization path:

1. Create a world with `sol world init myworld` (no `--source-repo`)
2. Manually edit world.toml to set `source_repo`
3. Run `sol world sync myworld`
4. Verify the managed clone was created at `$SOL_HOME/myworld/repo/`

## Verification

- `make build && make test` passes
- `grep -r 'refresh-mirror' cmd/ internal/` returns no matches (except old
  prompts in docs/)
- `grep -r 'MirrorPath\|SetupMirror\|RefreshMirror' internal/governor/` returns
  no matches
- Manual test:
  ```bash
  SOL_HOME=/tmp/sol-test bin/sol init --name=testworld --source-repo=<url-or-path> --skip-checks
  ls /tmp/sol-test/testworld/repo/   # managed clone exists
  bin/sol world sync testworld       # fetches latest
  bin/sol governor start --world=testworld
  cat /tmp/sol-test/testworld/governor/.claude/CLAUDE.md | grep 'repo'
  # Should reference ../repo/ for codebase research
  ```

## Guidelines

- The governor is now lighter — it doesn't manage its own clone. `sol world sync`
  is the single mechanism for keeping the repo current.
- The `MirrorDir` field in `GovernorClaudeMDContext` is repurposed to point at the
  managed repo. The field name could be renamed to `RepoDir` but that's cosmetic
  and can be done later — keep changes minimal.
- The governor session still runs in `$SOL_HOME/{world}/governor/` — that doesn't
  change. Only the code reading location changes.
- Do not remove `GovernorDir` or `BriefDir`/`BriefPath` — those are still used.

## Commit

`feat(world): add world sync, eliminate governor mirror (ADR-0014)`

# Prompt 02: Arc 3.5 — Consumer Adaptation

**Working directory:** ~/gt-src/
**Prerequisite:** Arc 3.5 prompt 01 complete, `make build && make test` passing

## Context

Read `CLAUDE.md` for project conventions.
Read `docs/decisions/0014-managed-world-repository.md` for the ADR.
Read `internal/config/config.go` for the new `RepoPath()` helper.
Read `internal/dispatch/dispatch.go` for `ResolveSourceRepo`, `DiscoverSourceRepo`, and `Cast`.
Read `internal/forge/forge.go` for `New()` and `EnsureWorktree`.
Read `internal/envoy/envoy.go` for `Create` and `ensureWorktree`.
Read `cmd/forge.go` for forge start and `openForge`.
Read `cmd/envoy.go` for envoy create (`--source-repo` flag).
Read `cmd/governor.go` for governor start (`--source-repo` flag).
Read `cmd/cast.go` for the cast command.
Read `cmd/caravan.go` for caravan launch.

## Task 1: Simplify `dispatch.ResolveSourceRepo()`

The current function checks world config then falls back to CWD git discovery.
With managed clones, it should check if the managed repo exists and return its
path.

In `internal/dispatch/dispatch.go`:

Replace `ResolveSourceRepo` (and remove `DiscoverSourceRepo`) with:

```go
// ResolveSourceRepo returns the path to the managed git clone for a world.
// Falls back to the world config source_repo and CWD git discovery for
// worlds that predate the managed clone system.
func ResolveSourceRepo(world string, cfg config.WorldConfig) (string, error) {
    // Prefer managed clone.
    repoPath := config.RepoPath(world)
    if info, err := os.Stat(repoPath); err == nil && info.IsDir() {
        return repoPath, nil
    }

    // Fallback: world config source_repo (legacy worlds without managed clone).
    if cfg.World.SourceRepo != "" {
        return cfg.World.SourceRepo, nil
    }

    // Fallback: discover from CWD (legacy convenience).
    repo, err := DiscoverSourceRepo()
    if err != nil {
        return "", fmt.Errorf("no managed repo at %s, no source_repo in world.toml, and not in a git repo", repoPath)
    }
    return repo, nil
}
```

Note the signature change: adds `world string` as first parameter.

Keep `DiscoverSourceRepo()` as-is — it's still used as a fallback for legacy
worlds and by `worldInitCmd`.

## Task 2: Update All `ResolveSourceRepo` Callers

The signature changed from `ResolveSourceRepo(cfg)` to
`ResolveSourceRepo(world, cfg)`. Update every caller:

### `cmd/cast.go` (line 37)

Change:
```go
sourceRepo, err := dispatch.ResolveSourceRepo(worldCfg)
```
To:
```go
sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
```

### `cmd/forge.go` — `forgeStartCmd` (line 51)

Change:
```go
sourceRepo, err := dispatch.ResolveSourceRepo(worldCfg)
```
To:
```go
sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
```

### `cmd/forge.go` — `openForge` function

Find the `dispatch.ResolveSourceRepo` call (around line 255) and update:

```go
sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
```

### `cmd/caravan.go` — `caravanLaunchCmd`

Find the `dispatch.ResolveSourceRepo` call (around line 401) and update:

```go
sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
```

`world` is already available as a variable in all these contexts. Check each
call site to confirm the variable name.

### `cmd/world.go` — `worldInitCmd` (line 62)

This call passes an empty config to discover from CWD. Update to:

```go
repo, err := dispatch.ResolveSourceRepo(name, config.WorldConfig{})
```

## Task 3: Remove `--source-repo` Flags from Envoy and Governor

With managed clones, envoy and governor should use the managed repo, not accept
a per-command override.

### `cmd/envoy.go`

Remove the `--source-repo` flag and variable:

1. Remove the `envoyCreateSourceRepo` variable declaration
2. Remove the flag registration in `init()` (the line with
   `envoyCreateCmd.Flags().StringVar(&envoyCreateSourceRepo, "source-repo", ...`)
3. In `envoyCreateCmd.RunE`, replace the source repo resolution block
   (lines ~45–55) with:

```go
sourceRepo, err := dispatch.ResolveSourceRepo(envoyCreateWorld, worldCfg)
if err != nil {
    return err
}
```

Remove the `worldCfg.World.SourceRepo` fallback and the "source repo required"
error — `ResolveSourceRepo` handles both.

### `cmd/governor.go`

Remove the `--source-repo` flag and variable:

1. Remove the `governorStartSourceRepo` variable declaration
2. Remove the flag registration in `init()` (the line with
   `governorStartCmd.Flags().StringVar(&governorStartSourceRepo, "source-repo", ...`)
3. In `governorStartCmd.RunE`, replace the source repo resolution block
   (lines ~47–52) with:

```go
sourceRepo, err := dispatch.ResolveSourceRepo(governorStartWorld, worldCfg)
if err != nil {
    return err
}
```

### Update tests

In `cmd/envoy_test.go`:

- `TestEnvoyCreate`: Remove `--source-repo=` from the command args. Instead,
  ensure the test's `world.toml` has `source_repo` set, OR set up a managed
  clone at `config.RepoPath("myworld")` before running the command.

  The simplest approach: after writing `world.toml`, create the managed clone:
  ```go
  // Create managed repo clone.
  repoPath := config.RepoPath("myworld")
  runGitCmd(t, sourceRepo, "clone", sourceRepo, repoPath)
  ```

  Where `runGitCmd` is a helper that runs git with the given args and fails the
  test on error.

- `TestEnvoyCreateNoSourceRepo`: Update to verify that envoy create fails when
  there's no managed clone AND no source_repo in config.

In `cmd/governor_test.go`:

- `TestGovernorStart`: Same pattern — remove `--source-repo=`, set up managed
  clone instead.

## Task 4: Update Integration Tests

Integration tests pass `--source-repo=` to envoy create and governor start.
Update them to set up the managed clone instead.

In `test/integration/helpers_test.go`, if there's a `setupWorldWithRepo` or
similar helper, update it to:

1. Run `sol world init <name> --source-repo=<path>` (this now clones to repo/)
2. Remove any explicit `--source-repo` from subsequent envoy/governor commands

In `test/integration/arc3_test.go`:

Find all `envoy create ... --source-repo=` and `governor start ... --source-repo=`
calls. Remove the `--source-repo` flag from these calls. The managed clone should
already exist from the `world init --source-repo=` call earlier in each test.

If a test creates envoy or governor without doing `world init --source-repo=`
first, add a clone setup step using the helper from `helpers_test.go`, or call
`setup.CloneRepo` directly.

Verify by grep: after changes, `--source-repo` should only appear in:
- `world init` and `sol init` commands
- `prefect.go` (consul source repo — separate concern)
- Old prompt files in `docs/prompts/`

## Task 5: Update `dispatch_test.go`

Update tests for `ResolveSourceRepo` to match the new signature:

- Tests that call `ResolveSourceRepo(cfg)` → `ResolveSourceRepo(world, cfg)`
- Add a test for the managed clone path: create `config.RepoPath("testworld")`
  as a directory, verify `ResolveSourceRepo("testworld", config.WorldConfig{})`
  returns it
- Keep the fallback test: verify that when no managed clone exists, the config
  value is returned

## Verification

- `make build && make test` passes
- `grep -r '\-\-source-repo' cmd/envoy.go cmd/governor.go` returns no matches
- `grep -r 'ResolveSourceRepo' cmd/` shows all callers pass `world` as first arg
- Existing cast/forge/caravan operations still work (they get source from
  managed clone or config fallback)

## Guidelines

- The fallback chain in `ResolveSourceRepo` (managed clone → config → CWD
  discovery) ensures backward compatibility with worlds created before this arc
- Do not remove `DiscoverSourceRepo` — it's still used as a fallback and by
  `worldInitCmd` for auto-detection
- Do not change forge or dispatch internals — they still receive `sourceRepo`
  as a string and use `git -C`. The only change is WHERE the string comes from.
- Do not change governor mirror handling yet — that's prompt 03

## Commit

`refactor(world): route all worktree consumers through managed repo path`

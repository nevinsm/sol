# Prompt 05: Arc 1 Review-5 — CLI Polish

You are fixing CLI-layer issues found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 04 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `cmd/consul.go` — consulRunCmd
- `cmd/handoff.go` — handoffCmd
- `cmd/resolve.go` — resolveCmd
- `cmd/feed.go` — followFeed function
- `cmd/agent.go` — agentCreateCmd
- `cmd/store.go` — storeCreateCmd, storeGetCmd, storeListCmd, storeUpdateCmd, storeCloseCmd, storeQueryCmd
- `cmd/store_dep.go` — storeDepAddCmd, storeDepRemoveCmd, storeDepListCmd
- `cmd/prime.go` — primeCmd
- `cmd/world.go` — worldDeleteCmd

---

## Task 1: Add cobra.NoArgs to commands that take no positional args

Several commands accept no positional arguments but don't validate this. Extra arguments are silently ignored.

**Fix:** Add `Args: cobra.NoArgs,` to each command struct. Insert after the `Short:` field:

| File | Command Variable | Line (approx) |
|------|-----------------|---------------|
| `cmd/consul.go` | `consulRunCmd` | after `Short:` on line 35 |
| `cmd/handoff.go` | `handoffCmd` | after `Short:` on line 23 |
| `cmd/resolve.go` | `resolveCmd` | after `Short:` on line 21 |

---

## Task 2: Fix context.Canceled comparison in feed

**File:** `cmd/feed.go`, `followFeed` function (around line 87)

```go
// Before:
if err == context.Canceled {

// After:
if errors.Is(err, context.Canceled) {
```

Add `"errors"` to the import block if not already present.

---

## Task 3: Use cmd.Context() in feed --follow

**File:** `cmd/feed.go`, `followFeed` function (around line 71)

The function creates `context.WithCancel(context.Background())` instead of using the cobra command context. This means `--follow` mode is not cancellable via cobra's context management.

**Fix:** Change `followFeed` to accept a context parameter, or change the caller to pass `cmd.Context()`. The simplest approach — accept a context:

```go
func followFeed(ctx context.Context, reader *events.Reader, opts events.ReadOpts) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigCh; cancel() }()

	// ... rest unchanged
```

Update the caller (in the `feedCmd` RunE) to pass `cmd.Context()`:

```go
return followFeed(cmd.Context(), reader, opts)
```

Add `"context"` to imports if not present.

---

## Task 4: Add GateTimeout validation to WorldConfig.Validate

**Note:** If this was already done in prompt 04, skip this task. Check `internal/config/world_config.go` — if `Validate()` already checks `Forge.GateTimeout`, this task is complete.

---

## Task 5: Fix world delete returning success without --confirm

**File:** `cmd/world.go`, `worldDeleteCmd` RunE (around line 302)

When `--confirm` is not passed, the command prints instructions and returns `nil` (exit code 0). This suggests success to scripts.

**Fix:** Return a non-zero exit code. Use a custom exit error:

```go
		if !worldDeleteConfirm {
			fmt.Printf("This will permanently delete world %q:\n", name)
			fmt.Printf("  - World database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
			fmt.Printf("  - World directory: %s\n", config.WorldDir(name))
			fmt.Printf("  - Agent records for world %q\n", name)
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}
```

Check if `exitError` is already defined in the `cmd` package (it was introduced for `mail check`). If so, reuse it. If not, find and reuse the existing type — search for `exitError` or `ExitCode` in the `cmd/` directory.

---

## Task 6: Add SilenceUsage to commands that do I/O

When a command fails due to a runtime error (database unavailable, network error), cobra prints the full usage help before the error. This is noisy and confusing for non-usage-related errors.

**Fix:** Add `SilenceUsage: true` to commands that perform I/O operations (database, file system, network). The convention is: if the command's `RunE` does real work beyond flag parsing, it should silence usage on error.

Add `SilenceUsage: true` to these command structs:

| File | Command Variable |
|------|-----------------|
| `cmd/agent.go` | `agentCreateCmd`, `agentListCmd` |
| `cmd/store.go` | `storeCreateCmd`, `storeGetCmd`, `storeListCmd`, `storeUpdateCmd`, `storeCloseCmd`, `storeQueryCmd` |
| `cmd/store_dep.go` | `storeDepAddCmd`, `storeDepRemoveCmd`, `storeDepListCmd` |
| `cmd/cast.go` | `castCmd` |
| `cmd/resolve.go` | `resolveCmd` |
| `cmd/handoff.go` | `handoffCmd` |
| `cmd/prime.go` | `primeCmd` |
| `cmd/world.go` | `worldInitCmd`, `worldDeleteCmd`, `worldStatusCmd`, `worldListCmd` |
| `cmd/escalation.go` | `escalateCmd`, `escalationListCmd` |
| `cmd/caravan.go` | `caravanCreateCmd`, `caravanStatusCmd`, `caravanCheckCmd` |

Do NOT add it to commands that already have it (check first — `consulStatusCmd`, `forgeRunGatesCmd`, `mailCheckCmd`, `sessionHealthCmd`, `workflowCurrentCmd`, `statusCmd` may already have it).

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Manual check: `bin/sol resolve extra-arg` should produce an error about unexpected args
- Manual check: `bin/sol world delete nonexistent` should output an error (not usage help)

## Commit

```
fix(cmd): arc 1 review-5 — NoArgs validation, context fixes, SilenceUsage, delete exit code
```

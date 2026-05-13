# internal/service Conventions

Conventions for implementing and extending OS service management in sol.
The package supports Linux (systemd) and Darwin (launchd); new Status or
lifecycle implementations must satisfy the same contract on both platforms.

## 1. `Status` exit-code contract (Linux and Darwin must match)

`Status()` returns:

| Return value          | CLI exit code | Meaning                                           |
|-----------------------|---------------|---------------------------------------------------|
| `nil`                 | 0             | All sol sphere daemons are running                |
| `ErrServiceDegraded`  | 2             | One or more daemons are stopped, failed, or unknown to the service manager |
| any other error       | 1             | The service manager itself could not be queried   |

The CLI layer (`cmd/service.go`) maps `ErrServiceDegraded` → exit 2 so
monitoring scripts can distinguish "degraded" from "command crashed".

**Both platforms must implement this three-way split.** When adding a
new platform, do not collapse all non-zero results into exit 1 — that
breaks scripts that distinguish degraded from crashed.

See the exit-code table in `CLAUDE.md` (Design Conventions → Exit code
conventions) for the sphere-wide policy.

## 2. `Restart` must roll back on partial-stop failure

`Restart` must not leave the sphere with fewer running daemons than it
started with. If `stopAll` fails mid-sequence, attempt to restart any
components that were already stopped before returning the error.

This is the capture-and-restore pattern applied to service lifecycle; the
reference implementations are `service_linux.go` and `service_darwin.go`
(`Restart` function in both files).

## 3. All five components (`Components`) must be treated uniformly

`service.Components` lists every sphere daemon managed as a system service.
`Install`, `Uninstall`, `Start`, `Stop`, `Restart`, and `Status` all iterate
over the full list. When adding a new component to sol:

1. Add it to `Components` in `service.go`.
2. Verify `Install`/`Uninstall` generates and removes the correct unit/plist.
3. Verify `Status` includes it in the degraded check.

Do not add per-component special cases inside the lifecycle functions — if a
component truly needs different handling, document why in a comment.

## 4. Linger / LaunchAgent persistence

`LingerEnabled()` is platform-specific:

- **Linux**: `loginctl enable-linger` must be set for user units to survive
  logout. `LingerEnabled()` checks this and `sol service install` warns if
  it is not set.
- **Darwin**: `LaunchAgents` persist for the logged-in session by default.
  `LingerEnabled()` always returns `true` on Darwin.

Do not add calls to `LingerEnabled()` outside the install flow — it is a
user-facing advisory check, not a runtime guard.

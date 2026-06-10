# Architectural Simplification Review

**Writ:** sol-022d856726275ac3  
**Reviewer:** Nova (outpost agent)  
**Date:** 2026-06-09  
**Commits reviewed:** 270660a → e181fd2 (10 commits, 2026-06-04 to 2026-06-06)

---

## Section 1: Orphaned References and Dead Code

**Status: WARN** (fixes applied where safe; one follow-up item)

### Findings

#### Fixed in this writ

| File | Issue | Fix |
|------|-------|-----|
| `internal/broker/provider.go` | Rate-limit detection `Provider` interface + `RateLimitSignal` struct — ADR-0040 explicitly removes this; nothing in production called `GetProvider` or `DetectRateLimit` | **Deleted** |
| `internal/adapter/claude/provider.go` | Dead `broker.Provider` impl for Claude (`DetectRateLimit`, `RegisterProvider` init) | **Deleted** |
| `internal/adapter/claude/provider_test.go` | Tests for deleted claude provider | **Deleted** |
| `internal/adapter/codex/provider.go` | Dead `broker.Provider` impl for Codex (`DetectRateLimit`, `RegisterProvider` init) | **Deleted** |
| `internal/adapter/codex/provider_test.go` | Tests for deleted codex provider | **Deleted** |
| `internal/forge/forge.go:23` | `DailySpendByAccount` in `forge.WorldStore` interface — method was never called by forge production code | **Removed from interface** |
| `internal/sentinel/sentinel.go:107` | `DailySpendByAccount` in `sentinel.WorldStore` interface — method was never called by sentinel production code | **Removed from interface** |
| `internal/startup/startup.go:337` | Comment said "credentials stored there are invalidated by account rotation" — stale after ADR-0040 | **Updated** |
| `cmd/agent.go:235-238` | Comment referenced `internal/account` (deleted package) and "broker-managed .account metadata" | **Updated** |

#### Flagged for follow-up (not fixed here — behavior change)

- **`cmd/agent.go:239-246` — `readAgentAccountBinding` + ACCOUNT column** (severity: low)  
  The `.account` file this function reads was written by `internal/account` (deleted). New agents will never have it; the function always returns "". The `ACCOUNT` column in `sol agent list` will perpetually show `-` for all agents. Removing it changes CLI output format (column count, headers), which may break scripts. Left in place with updated comment; follow-up writ should remove once old agents drain.

#### No action required

- `internal/config/world_config.go:121` — `DefaultAccount` field is **intentionally retained** as a telemetry label. `docs/configuration.md:32` correctly documents it as "Telemetry label for agent sessions — forwarded to the ledger for token usage attribution. Does not affect routing or dispatch."
- `internal/startup/startup.go:88,291-293` — `opts.Account` / `resolvedAccount` — retained for telemetry attribution only (passed to `TelemetryEnv`). Comments explain this.
- `internal/store/ledger.go:234-246` — `DailySpendByAccount` remains in the store implementation and `store.LedgerReader` interface. It is used by `sol cost` reporting through direct store access (not through sentinel/forge interfaces).
- `cmd/agent.go:236` comment about "internal/account" — updated in this writ.
- `internal/adapter/adapter.go:83` — `SHOULD set account when the session uses a specific account` — this is a telemetry labeling hint, not account routing. Accurate for the telemetry-only use case that remains.

**Severity of remaining items:** low

---

## Section 2: New ADR Review

**Status: PASS**

**File:** `docs/decisions/0040-architectural-simplification.md`

### Checklist

- [x] **Status header is `Proposed`** — line 3: `Status: Proposed`
- [x] **Thin runtime contract** — Section 1 covers RuntimeAdapter collapse with clear reasoning
- [x] **Operator-managed credentials** — Section 2 with Claude/Codex examples; clear "sol never stores tokens"
- [x] **Per-agent config dir isolation retained** — Section 3 explicitly preserves ADR-0018
- [x] **No autonomous rate-limit recovery** — Section 4 explicitly accepts idle-until-operator trade-off with rationale
- [x] **ADR-0019 superseded** — "Supersession" section explicitly supersedes ADR-0019; also reflected in `docs/decisions/README.md` index
- [x] **Listed in `docs/decisions/README.md`** — entry 0040 present with correct title and status
- [x] **ADR-0036 amended note** — "ADR-0036 is amended accordingly" mentioned in Consequences section
- [x] **Alternatives section** — three alternatives considered with clear rejection reasoning
- [x] **`sol cost` retained as reporting-only** — Section 5 covers this explicitly

**Assessment:** The ADR faithfully captures the simplification direction. The supersession chain (0019 → 0040, 0031 amended) is clearly documented. The accepted trade-off (agents idle after rate-limit until operator intervenes) is explicit with clear rationale.

---

## Section 3: Credential Symlink Simplification

**Status: PASS**

### Claude Adapter (`internal/adapter/claude/claude.go`)

- [x] **`EnsureConfigDir` creates symlink to `~/.claude/.credentials.json`** — lines 284-297. Symlink target is `filepath.Join(home, ".claude", ".credentials.json")`.
- [x] **Created unconditionally** — no account-aware target resolution; simple `os.Remove` + `os.Symlink`
- [x] **Idempotent** — existing symlink is removed before creating the new one
- [x] **Created once at spawn, never swapped** — no rotation logic anywhere in the file
- [x] **Per-agent config dirs still created** — `config.EnsureClaudeConfigDir(worldDir, role, agent)` called first (ADR-0018 preserved)
- [x] **No account-aware imports** — no imports of `internal/account`, `internal/quota`, `internal/budget`

### Codex Adapter (`internal/adapter/codex/codex.go`)

- [x] **`EnsureConfigDir` creates symlink to `~/.codex/auth.json`** — lines 700-712. Same pattern: `os.Remove` + `os.Symlink`
- [x] **No account-aware target resolution** — symlink is unconditional
- [x] **Per-agent CODEX_HOME directory still created** — `os.MkdirAll(agentHome, 0o755)` at line 619

---

## Section 4: Sentinel Rate-Limit Removal

**Status: PASS** (with one dead interface method fixed in this writ)

### Findings

- [x] **No `internal/quota` import** — `internal/sentinel/sentinel.go` imports confirmed clean; no quota package
- [x] **No `quota.*` calls** — grep confirms zero references
- [x] **No `broker.Provider` / `DetectRateLimit` calls** — removed from broker (fixed in this writ)
- [x] **Stalled-agent detection intact** — patrol loop, output hashing, AI assessment, escalation path all preserved; rate-limit detection was a separate branch now cleanly removed
- [x] **No dead variables/structs** — no commented-out rate-limit structs or unused fields
- [x] **`DailySpendByAccount` removed from `sentinel.WorldStore` interface** — was declared but never called (fixed in this writ)

### Tests (`internal/sentinel/sentinel_test.go`)

Tests cover the remaining sentinel behaviors:
- Stalled agent detection via output hash unchanged
- Session respawn after crash
- Recast after failed MR
- Idle agent reaping
- Orphaned "working" agent recovery

No test references `DailySpendByAccount` or rate-limit detection paths. Test suite passes cleanly (38.3s, no failures).

---

## Section 5: Dispatch Path

**Status: PASS**

### Findings

- [x] **No `account.ResolveAccount` calls** — `internal/dispatch/dispatch.go` has no account imports
- [x] **No `budget.CheckAccountBudget` calls** — no budget imports
- [x] **No `--account` flag on `sol cast`** — grep of `cmd/cast.go` confirms no account flag
- [x] **`startup.Opts.Account` retained for telemetry only** — `internal/startup/startup.go:88` with comment "account override (empty = use world default)"; resolved value passed only to `TelemetryEnv` at line 402
- [x] **No silent fallback** — if no account is configured, telemetry simply omits the `account=` OTEL attribute; dispatch still proceeds

---

## Section 6: Broker Liveness Simplification

**Status: PASS** (after fixes applied in this writ)

### Findings

#### Fixed: Rate-limit detection interface removed

The `broker.Provider` interface with `DetectRateLimit` and `RateLimitSignal` was still present in `internal/broker/provider.go` after the simplification commits. Neither was called from production code — the interface was dead code orphaned by the simplification. **Deleted** in this writ along with both adapter provider implementations.

#### Post-fix state

- [x] **`internal/broker/provider.go` deleted** — rate-limit detection interface gone
- [x] **Per-provider account state gone** — broker only tracks `RuntimeLiveness{Runtime, OK, LastProbe}` in `Heartbeat`
- [x] **`sol broker status` reports simple liveness** — `broker.go` + `health.go` show per-runtime OK/down state
- [x] **`sol status` integration confirmed working** — broker reports `claude ok` style output
- [x] **Broker is a thin liveness probe** — `broker.go` probes runtime binary via `--version` invocation; `health.go` aggregates into `HealthHealthy` / `HealthDown`

#### Note on ADR-0036

ADR-0040 says "ADR-0036 is amended accordingly." ADR-0036 (`0036-broker-provider-interface.md`) itself has not been updated to show an `Amended` status — it still reads `Accepted`. This is a minor doc gap.

**Severity:** low — the behavior change is correctly implemented; only the ADR status header is stale.

---

## Section 7: Forge Branch Sweep

**Status: PASS**

### Implementation (`internal/forge/sweep.go`)

- [x] **`SweepBranches` function present** — well-documented with clear deletion logic
- [x] **`--include-closed-orphans` flag** — `forgeSweepIncludeClosedOrphans` bool in `cmd/forge.go:86`, passed to `SweepBranches(ctx, includeClosedOrphans, dryRun)`
- [x] **`--dry-run` flag** — `forgeSweepDryRun` bool in `cmd/forge.go:87`; when set, report is populated but no deletions performed
- [x] **Skips worktree-checked-out branches** — `listCheckedOutBranches` reads `git worktree list --porcelain`; checked-out branches preserved with reason "worktree-checked-out"
- [x] **Skips envoy persistent branches** — `parseWritID` returns "" for branches without "sol-" suffix (e.g., `envoy/world/Envoy`); these get `Preserved{Reason: "no-writ-id"}`
- [x] **Single upfront fetch** — `git fetch origin` before all checks; subsequent operations use cached remote state

### Tests (`internal/forge/sweep_test.go`)

All required cases covered:

| Test | Case |
|------|------|
| `TestSweepBranches_MergedWrit` | Writ ID in target branch commit history → deleted |
| `TestSweepBranches_ClosedOrphanIncluded` | Writ closed + `includeClosedOrphans=true` → deleted |
| `TestSweepBranches_ClosedOrphanExcluded` | Writ closed + `includeClosedOrphans=false` → preserved |
| `TestSweepBranches_WorktreeCheckedOut` | Branch checked out in worktree → preserved |
| `TestSweepBranches_NotMergedNotClosed` | Writ still open, not merged → preserved |
| `TestSweepBranches_EnvoyBranchWithWritID` | Envoy branch with writ-ID suffix → handled correctly |
| `TestSweepBranches_MixedCandidates` | Mix of cases in one sweep |
| `TestSweepBranches_RealGit*` | Integration tests with real git repo |

- [x] **`docs/cli.md` updated** — `sol forge sweep` entry present at line 1137 and expanded docs at line 1248

---

## Section 8: Build and Test

**Status: PASS**

### Results

| Gate | Result | Notes |
|------|--------|-------|
| `make build` | ✅ PASS | Compiles cleanly |
| `make test` | ✅ PASS | 40 packages, 0 failures |

### Test run output (selected packages)

```
ok  github.com/nevinsm/sol/internal/broker      0.033s
ok  github.com/nevinsm/sol/internal/adapter/claude   0.425s
ok  github.com/nevinsm/sol/internal/adapter/codex    0.525s
ok  github.com/nevinsm/sol/internal/sentinel    38.301s
ok  github.com/nevinsm/sol/internal/forge       25.218s
ok  github.com/nevinsm/sol/internal/startup      8.604s
ok  github.com/nevinsm/sol/test/integration    443.701s
```

No test failures. Known flaky tests (`TestDAGWorkflowE2E`, `TestMassDeathDegradation`) excluded from default `make test` via `make test-flaky` gate.

---

## Section 9: Documentation Alignment

**Status: PASS**

### `docs/principles.md`

- [x] **DEGRADE table** — no account rotation or quota detection in the table
- [x] **Sentinel row** — describes stalled-agent detection, not credential rotation
- [x] **No active budget/quota/rotation references** — grep confirms clean

### `docs/failure-modes.md`

- [x] **"Credential Exhaustion" section** — clearly states "Sol does not rotate credentials automatically — this is an operator concern" (line 160)
- [x] **Recovery path** — operator updates `.env`, restarts sessions; no account rotation
- [x] **"There is no multi-account routing in sol"** — explicit statement at line 171

### `CLAUDE.md`

- [x] **Broker description** — "Sphere-level liveness probe for AI provider runtimes (claude, codex) — discovers configured runtimes, probes availability, and surfaces health status" — matches simplified broker
- [x] **No Account / Quota / Budget components** — Components list clean
- [x] **Adapter component** — accurately describes RuntimeAdapter with no mention of credential management

### `docs/cli.md`

- [x] **No `sol account` entries** — confirmed
- [x] **No `sol quota` entries** — confirmed  
- [x] **No `sol budget` entries** — confirmed
- [x] **`sol forge sweep` present** — at lines 1137 and 1248 with full flag documentation

### `docs/configuration.md`

- [x] **`default_account` documented as telemetry label** — line 32: "Telemetry label for agent sessions — forwarded to the ledger for token usage attribution. Does not affect routing or dispatch." Accurate for current behavior.

---

## Summary of All Issues

| # | Location | Issue | Severity | Action |
|---|----------|-------|----------|--------|
| 1 | `internal/broker/provider.go` | Rate-limit `Provider` interface not removed | high | **Fixed** (deleted) |
| 2 | `internal/adapter/claude/provider.go` + test | Dead `broker.Provider` impl | high | **Fixed** (deleted) |
| 3 | `internal/adapter/codex/provider.go` + test | Dead `broker.Provider` impl | high | **Fixed** (deleted) |
| 4 | `internal/forge/forge.go:23` | `DailySpendByAccount` in interface, never called | medium | **Fixed** (removed) |
| 5 | `internal/sentinel/sentinel.go:107` | `DailySpendByAccount` in interface, never called | medium | **Fixed** (removed) |
| 6 | `internal/startup/startup.go:337` | Stale "account rotation" comment | low | **Fixed** |
| 7 | `cmd/agent.go:235-238` | Stale comment referencing `internal/account` | low | **Fixed** (updated) |
| 8 | `cmd/agent.go:239-246` | `readAgentAccountBinding` + ACCOUNT column always "-" | low | **Flagged for follow-up** |
| 9 | `docs/decisions/0036-broker-provider-interface.md` | Status header not updated to `Amended` | low | **Flagged for follow-up** |

---

## Follow-Up Items for Future Writs

### FU-1: Remove `readAgentAccountBinding` + ACCOUNT column (low priority)
**Location:** `cmd/agent.go:232-246`, `cmd/agent.go:148-152`, `cmd/agent.go:203`  
**Issue:** `readAgentAccountBinding` reads `.account` file never written by current code (written by deleted `internal/account`). The ACCOUNT column in `sol agent list` permanently shows `-`. Remove the function and column to clean up the CLI output.  
**Why deferred:** Removing a column from tabular CLI output is a breaking change for scripts.

### FU-2: Mark ADR-0036 as Amended (low priority)
**Location:** `docs/decisions/0036-broker-provider-interface.md`  
**Issue:** ADR-0040 says "ADR-0036 is amended accordingly" but the ADR-0036 file still has `Status: Accepted`. Should be updated to `Status: Amended by ADR-0040` with a pointer note.  
**Why deferred:** Documentation-only; no behavior impact.

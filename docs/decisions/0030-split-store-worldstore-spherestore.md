# ADR-0030: Split Store into WorldStore and SphereStore

Status: accepted; migration complete — `Store` shim and `OpenWorld`/`OpenSphere` have been removed; all consumers use `WorldStore`/`SphereStore` directly.

## Context

`internal/store.Store` is a single type with ~100 exported methods across 17 files (5,300+ lines). It wraps two distinct SQLite databases — world (writs, MRs, dependencies, token usage, history) and sphere (agents, messages, escalations, caravans, worlds) — but the type system does not distinguish between them. `OpenWorld()` and `OpenSphere()` both return `*Store`. Calling a sphere method on a world store is a runtime surprise, not a compile error.

Additionally, 14+ consumer packages each define their own interface subsets of `Store` (e.g., `dispatch.WorldStore`, `forge.WorldStore`, `sentinel.SphereStore`). These overlap ~80% but drift independently — no canonical interfaces exist.

### Problems

1. **No compile-time boundary enforcement**: `s.GetAgent(id)` compiles just fine even when `s` was opened with `OpenWorld`. The bug only surfaces at runtime when the SQL query finds no `agents` table.

2. **Method surface too wide**: Every consumer sees all ~100 methods regardless of which database they actually use. This makes it harder to understand which methods are valid in a given context.

3. **Interface proliferation**: Each consumer package defines its own Store interface subset tailored to its needs. These drift independently and require constant reconciliation when new methods are added.

### Options Considered

**Option A — Two concrete types with embed-then-split migration**
Define `WorldStore` and `SphereStore` as distinct Go types. Redefine `Store` to embed both as a transitional shim so existing callers keep working during migration. Remove `Store` in a later writ once all consumers have migrated.

**Option B — Single type, keep as-is**
Accept the current state. Low immediate cost but problems compound as the codebase grows.

**Option C — Full domain decomposition (8+ sub-packages)**
Split store into sub-packages: `store/writs`, `store/agents`, `store/messages`, etc. Provides maximum isolation but cross-domain operations (writ+MR queries, caravan+writ readiness) would require either cross-package calls or a re-aggregation layer. At current scale this adds more complexity than it removes.

## Decision

Implement **Option A**: split `*Store` into `*WorldStore` and `*SphereStore` as distinct Go types, each scoped to its database. Use an embed-then-split migration strategy:

1. **New types** (`internal/store/`):
   - `WorldStore` wraps the per-world SQLite database (writs, MRs, dependencies, ledger, history). (Originally also held an `agent_memories` table, dropped in schema V13 when the brief system was retired.)
   - `SphereStore` wraps the sphere-wide SQLite database (agents, messages, escalations, caravans, worlds).
   - Each type has its own `Close()`, `DB()`, `Path()`, `Checkpoint()`, `SchemaVersion()`, and migration methods.

2. **New constructors**:
   - `OpenWorldStore(world string) (*WorldStore, error)` — returns a type-safe world handle.
   - `OpenSphereStore() (*SphereStore, error)` — returns a type-safe sphere handle.

3. **Transitional shim**: The old `Store` type is redefined to embed both:
   ```go
   type Store struct {
       db   *sql.DB  // depth-0 field for backward compat
       path string
       WorldStore
       SphereStore
   }
   ```
   `OpenWorld()` and `OpenSphere()` continue to return `*Store` (wrapping the appropriate inner type), preserving backward compatibility for all existing consumers during migration. The depth-0 `db` and `path` fields shadow the promoted fields from the embedded types, ensuring existing code that accesses `s.db` directly continues to compile.

4. **Canonical interfaces** (`internal/store/interfaces.go`): Define composable interface types that consumer packages will adopt, eliminating per-package interface drift:
   - World-scoped: `WritReader`, `WritWriter`, `MRReader`, `MRWriter`, `DepReader`, `DepWriter`, `LedgerReader`, `LedgerWriter`, `HistoryStore`, `AgentMemoryStore`
   - Sphere-scoped: `AgentReader`, `AgentWriter`, `CaravanReader`, `CaravanWriter`, `CaravanDepReader`, `CaravanDepWriter`, `MessageStore`, `EscalationStore`, `WorldRegistry`
   - Compile-time checks (`var _ X = (*WorldStore)(nil)`) ensure implementations stay current.

5. **Migration path**: Consumer packages migrate from `*Store` to either `*WorldStore` or `*SphereStore` (or a canonical interface) in a subsequent writ. Once all consumers have migrated, `Store`, `OpenWorld`, and `OpenSphere` are deleted.

### Why Not Option C

Cross-domain operations exist today and are load-bearing:
- `CheckCaravanReadiness` on `SphereStore` opens world databases to check writ status — it traverses the sphere/world boundary.
- Forge and Sentinel each need both world (writ/MR) and sphere (agent/message) data within the same request cycle.

Eight sub-packages with their own DB handles would require either duplicated connections or an awkward aggregation layer to support these operations. The two-type split maintains a clean boundary while keeping cross-domain calls straightforward.

## Consequences

**Positive**:
- Compile-time enforcement of database boundaries: `(*WorldStore).GetAgent()` does not exist.
- Halved method surface per type — `*WorldStore` exposes only world methods, `*SphereStore` only sphere methods.
- Canonical interfaces in one place; consumer packages adopt them incrementally without interface drift.
- New constructors (`OpenWorldStore`, `OpenSphereStore`) can be used immediately by new code.

**Negative / Trade-offs**:
- The `Store` embedding shim is an intermediate inconsistency: `*Store` satisfies both world and sphere interfaces even though a single `Store` instance cannot validly be used as both.
- Migration requires touching every consumer package — deferred to the next writ.
- `OpenWorld` and `OpenSphere` remain and return `*Store` until migration completes; their deprecation comment reminds callers to prefer the new constructors.

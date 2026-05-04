# docvalidate — documentation drift checks

`internal/docvalidate` runs a battery of independent checks that compare the
project's documentation against its source code. Each check has one piece of
"ground truth" in code (a Go slice, a TOML manifest, an ADR index) and one or
more docs that describe it; when the docs disagree, the check emits a
`Finding` with file:line context and an expected-vs-actual message.

This package is invoked from `sol docs validate` (which `make docs-validate`
calls) and is intended to run on every CI build.

## Checks

### `adr-refs`

**Ground truth:** the status table in `docs/decisions/README.md`.

For every superseded ADR (a row whose Status column reads
`Superseded by ADR-NNNN`) the check walks every Markdown file under `docs/`
(except the ADR files themselves) plus `CLAUDE.md`, and flags any reference
that still cites the superseded ADR — pointing at the canonical replacement.

### `workflow-steps`

**Ground truth:** `[[steps]]` count in
`{repoRoot}/.sol/workflows/<name>/manifest.toml` and
`{repoRoot}/internal/workflow/defaults/<name>/manifest.toml`. Project-tier
shadows embedded-tier, matching `sol workflow list` semantics.

The check walks `docs/workflows.md` and `docs/operations.md` for prose
claims of the form `manifest (N steps)` and flags any disagreement with the
nearest preceding workflow heading. Orphan claims (no preceding heading) are
also flagged.

### `recovery-matrix`

**Ground truth:** the package-level `Components` slice in
`internal/service/service.go`, parsed with `go/parser` (not regex — multi-line
declarations and added comments must not break the check).

Every component name must have a row in the `Recovery Matrix` table in
`docs/failure-modes.md`. Comparison is case-insensitive (`broker` ↔ `Broker`).

### `heartbeat-paths`

**Ground truth:** the canonical `HeartbeatPath()` (or `heartbeatPath()`)
function in each `internal/<pkg>/heartbeat*.go`, parsed with `go/ast` and
rendered as a path template:

| Go expression                      | Rendered                |
|-----------------------------------|-------------------------|
| `config.Home()`                   | `$SOL_HOME`             |
| `config.RuntimeDir()`             | `$SOL_HOME/.runtime`    |
| identifier `world` (param)        | `{world}`               |
| `filepath.Join(a, b, c)`          | `a/b/c`                 |
| string literal `"foo"`            | `foo`                   |

The rendered path is compared against the heartbeat-path table in
`docs/operations.md`. Components with no doc row are also flagged.

### `persona-archetypes`

**Ground truth:** the keys of `var knownDefaults = map[string]bool{...}` in
`internal/persona/defaults.go`, parsed with `go/parser`.

Every persona template name (the first column of the `Built-in templates`
table in `docs/personas.md` and `docs/naming.md`) must be a registered key.
The archetype label (Polaris, Meridian, …) is reported alongside the name
for diagnostic clarity but is not itself a registry key.

### `acceptance-tests`

**Ground truth:** every top-level `func TestX(t *testing.T)` declared
anywhere under `test/integration/`, parsed with `go/parser` (not a file
grep — a build-tagged or commented-out declaration must not be counted).

For every checked-off (`[x]`) line in `test/integration/LOOP*_ACCEPTANCE.md`,
each backtick-wrapped `TestFooBar` reference must correspond to a real test
function. Unchecked (`[ ]`) lines are ignored.

## Adding a check

1. Add `mycheck.go` with a `func CheckMyCheck(repoRoot string) ([]Finding, error)`.
2. Register it in `AllChecks()` in `run.go`.
3. Add `mycheck_test.go` with synthetic fixtures under `t.TempDir()`.
4. Document it here.

Each check should run in O(seconds) on this codebase: pure Go, regex + AST
walks only, no shelling out.

## Running

```bash
# All checks at once.
sol docs validate

# Or via make (builds the binary first).
make docs-validate
```

A non-zero exit code means at least one check failed. Errors include
file:line + expected-vs-actual context so downstream tooling can scrape
output for follow-up work.

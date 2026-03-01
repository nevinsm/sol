# Prompt 10: Arc 2 — Integration Tests + Acceptance

You are completing Arc 2 (Operator Onboarding) with integration tests,
documentation updates, and final acceptance verification.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompts 01–09 are complete (doctor, init,
status overhaul).

Read the existing documentation first:
- `docs/decisions/` — all existing ADRs for format reference
- `docs/arc-roadmap.md` — current arc roadmap (has Arc 2 section)
- `CLAUDE.md` — project root instructions
- `test/integration/helpers_test.go` — test helper patterns

---

## Task 1: Integration Test Suite

**Create** `test/integration/arc2_test.go`.

This test file covers the end-to-end operator onboarding flow. Use the
existing test helpers (`setupTestEnv`, `runGT`, `gtBin`).

### Doctor Tests

```go
func TestDoctorEndToEnd(t *testing.T)
    // Run: sol doctor
    // Verify: exit code 0 (tmux + git available in CI)
    // Verify: output contains "✓ tmux"
    // Verify: output contains "✓ git"
    // Verify: output contains "✓ sqlite_wal"
    // Verify: output contains "All checks passed"

func TestDoctorJSONEndToEnd(t *testing.T)
    // Run: sol doctor --json
    // Parse output as JSON.
    // Verify: top-level "checks" array exists.
    // Verify: each check has "name", "passed", "message" fields.
    // Verify: tmux, git, sqlite_wal checks are present and passed.

func TestDoctorBeforeSOLHome(t *testing.T)
    // Set SOL_HOME to a non-existent path.
    // Run: sol doctor
    // Verify: succeeds (doctor works before SOL_HOME exists).
    // Verify: SOL_HOME directory was NOT created as a side effect.
```

### Init Tests

```go
func TestInitFlagModeEndToEnd(t *testing.T)
    // Set SOL_HOME to a non-existent path inside t.TempDir().
    // Run: sol init --name=myworld --skip-checks
    // Verify: exit code 0
    // Verify: output contains "sol initialized successfully"
    // Verify: SOL_HOME directory exists
    // Verify: $SOL_HOME/.store/ exists
    // Verify: $SOL_HOME/.runtime/ exists
    // Verify: $SOL_HOME/myworld/world.toml exists
    // Verify: $SOL_HOME/.store/myworld.db exists
    // Verify: $SOL_HOME/myworld/outposts/ exists

func TestInitWithSourceRepoEndToEnd(t *testing.T)
    // Create a temp dir as source repo.
    // Run: sol init --name=myworld --source-repo=<tempdir> --skip-checks
    // Verify: world.toml contains source_repo value.
    // Read world.toml and check the source_repo field.

func TestInitAlreadyInitializedEndToEnd(t *testing.T)
    // Run: sol init --name=myworld --skip-checks → success
    // Run: sol init --name=myworld --skip-checks → error
    // Verify: error contains "already initialized"

func TestInitThenFullFlow(t *testing.T)
    // Run: sol init --name=myworld --skip-checks
    // Run: sol store create --world=myworld --title="First task"
    // Run: sol world list → output contains "myworld"
    // Run: sol status myworld → exit 0
    // Run: sol status → output contains "myworld"
    // Run: sol world status myworld → output contains "Config:"
    // This verifies the full operator flow from init to first status check.
```

### Status Tests

```go
func TestStatusSphereEndToEnd(t *testing.T)
    // Init a world.
    // Run: sol status (no args)
    // Verify: output contains "Sol Sphere"
    // Verify: output contains the world name
    // Verify: output contains "Processes"
    // Verify: exit code 0

func TestStatusSphereJSONEndToEnd(t *testing.T)
    // Init a world.
    // Run: sol status --json
    // Parse as JSON.
    // Verify: "sol_home" field present.
    // Verify: "worlds" is an array with 1 entry.
    // Verify: "health" field present.

func TestStatusSphereMultipleWorlds(t *testing.T)
    // Init 2 worlds (sol init for first, sol world init for second).
    // Run: sol status
    // Verify: output contains both world names.

func TestStatusWorldWithLipgloss(t *testing.T)
    // Init a world, create a work item.
    // Run: sol status myworld
    // Verify: output contains "Processes" section.
    // Verify: output contains "Merge Queue" section.
    // Verify: exit code reflects health.

func TestStatusWorldJSONUnchanged(t *testing.T)
    // Init a world.
    // Run: sol status myworld --json
    // Parse as JSON.
    // Verify: same fields as before (world, prefect, forge, agents, etc.)
    // This ensures JSON format backward compatibility.

func TestStatusEmptySphere(t *testing.T)
    // Set up SOL_HOME but don't init any worlds.
    // Run: sol status
    // Verify: output contains "No worlds initialized."
```

### Cross-Feature Tests

```go
func TestDoctorThenInit(t *testing.T)
    // Run: sol doctor → all pass
    // Run: sol init --name=myworld --skip-checks → success
    // Run: sol status → shows world
    // This is the canonical operator onboarding flow.

func TestInitRunsDoctor(t *testing.T)
    // Run: sol init --name=myworld (no --skip-checks)
    // Depending on environment:
    //   - If doctor passes: init succeeds.
    //   - If doctor fails: error includes check details.
    // Verify: either way, the behavior is correct.
```

---

## Task 2: Update Arc Roadmap

**Modify** `docs/arc-roadmap.md`.

Update the Arc 2 section with completion notes. Follow the pattern from
Arc 1's completion notes:

```markdown
## Arc 2: Operator Onboarding

Make the system approachable for first-time operators.

- `sol doctor` — validate prerequisites (tmux, git, claude, writable dirs, SQLite WAL)
- `sol init` — guided first-time setup (create SOL_HOME, first world)
  - Flag mode: `sol init --name=<world> [--source-repo=<path>]`
  - Interactive mode: huh prompts when stdin is a TTY
  - Guided mode: `sol init --guided` — ephemeral Claude session
- Actionable error messages when prerequisites fail
- Init runs doctor by default (`--skip-checks` to bypass)

### Status Overhaul

Polished status output using Charmbracelet lipgloss. New sphere-level
overview for system-wide visibility.

- `sol status` (no args) — sphere overview: sphere processes (prefect,
  consul, chronicle), worlds table with per-world summary, open caravans
- `sol status <world>` — updated rendering with lipgloss styling
- Charmbracelet lipgloss for section headers, tables, colored status
  indicators. `--json` bypasses all styling.
- Rendering separated from data gathering (`status/render.go`)
- Consul status added to sphere process checks
- Sphere health: aggregate of all world health + sphere process health

Role-aware sections (outposts/envoys/governor) land with Arc 3 when
those roles exist.

**Acceptance:** A new operator can go from zero to first successful `cast` with
clear guidance at every step. `sol doctor` catches all common setup issues.
`sol status` gives a system-wide overview at a glance.

**Status:** Complete.
- ADR-0012: Charmbracelet adoption (lipgloss + huh)
- `internal/doctor/`: prerequisite check engine (5 checks)
- `internal/setup/`: first-time setup engine
- `internal/status/render.go`: lipgloss-styled rendering
- `sol doctor`, `sol init`, `sol status` (sphere overview)
```

---

## Task 3: Update CLAUDE.md

**Modify** `CLAUDE.md` (project root).

Add Arc 2 entries. Keep changes minimal — only what a new developer
needs to know.

### Key Concepts additions

Under the existing Key Concepts section, add:

```markdown
- **Doctor**: Prerequisite validator — checks tmux, git, claude, SOL_HOME, SQLite WAL
- **Init**: First-time setup — creates SOL_HOME, first world (flag/interactive/guided modes)
```

### Components (built) additions

Add to the Components (built) section:

```markdown
- **Doctor**: Prerequisite check engine (`internal/doctor/`)
```

Update the status description if needed:

```markdown
- **Status**: Sphere overview + per-world detail, lipgloss-styled rendering
```

### Conventions additions

Under existing Conventions, add if not already present:

```markdown
- Dependencies: charmbracelet/lipgloss (terminal styling), charmbracelet/huh (interactive prompts)
```

---

## Task 4: Acceptance Verification

Run the full acceptance sweep. Every check must pass.

### Build and test

```bash
make build && make test
```

### Doctor flow

```bash
export SOL_HOME=/tmp/sol-arc2-test
rm -rf /tmp/sol-arc2-test

# Doctor runs before SOL_HOME exists
bin/sol doctor
echo "Exit code: $?"

# JSON output
bin/sol doctor --json | python3 -m json.tool
```

### Init flow

```bash
# Flag mode
bin/sol init --name=myworld --skip-checks
test -f /tmp/sol-arc2-test/myworld/world.toml && echo "PASS: world.toml"
test -f /tmp/sol-arc2-test/.store/myworld.db && echo "PASS: world DB"
test -d /tmp/sol-arc2-test/myworld/outposts && echo "PASS: outposts dir"
test -d /tmp/sol-arc2-test/.store && echo "PASS: .store dir"
test -d /tmp/sol-arc2-test/.runtime && echo "PASS: .runtime dir"

# Already initialized
bin/sol init --name=myworld --skip-checks 2>&1 | grep -q "already initialized" && echo "PASS: idempotent"
```

### Status flow

```bash
# Create some data
bin/sol store create --world=myworld --title="Test item 1"
bin/sol store create --world=myworld --title="Test item 2"

# Sphere overview
bin/sol status
echo "PASS: sphere overview"

# World detail
bin/sol status myworld
echo "PASS: world detail"

# JSON output
bin/sol status --json | python3 -m json.tool
bin/sol status myworld --json | python3 -m json.tool

# Add a second world
bin/sol world init alpha
bin/sol status
# Should show both worlds
```

### World operations still work

```bash
# Verify existing commands unbroken
bin/sol world list
bin/sol world status myworld
bin/sol store list --world=myworld
```

### Cross-feature flow

```bash
# The canonical operator onboarding flow
rm -rf /tmp/sol-arc2-fresh
export SOL_HOME=/tmp/sol-arc2-fresh
bin/sol doctor
bin/sol init --name=myproject --skip-checks
bin/sol store create --world=myproject --title="First task"
bin/sol status
bin/sol status myproject
echo "PASS: full operator flow"
```

### Cleanup

```bash
rm -rf /tmp/sol-arc2-test /tmp/sol-arc2-fresh
```

### Grep verification

```bash
# No raw printWorldStatus calls remaining
grep -rn 'printWorldStatus' cmd/*.go
# Should be gone (replaced by RenderWorld)

# lipgloss import only in render.go
grep -rn 'charmbracelet/lipgloss' internal/
# Should only appear in status/render.go

# huh import only in init
grep -rn 'charmbracelet/huh' cmd/ internal/
# Should only appear in cmd/init.go

# Doctor checks are self-contained
grep -rn 'internal/doctor' cmd/ internal/
# Should appear in cmd/doctor.go, cmd/init.go, internal/setup/setup.go
```

---

## Task 5: Memory Update

If auto-memory is in use, update `MEMORY.md` with Arc 2 completion:

```markdown
## Arc 2 — Operator Onboarding — COMPLETE
- ADR-0012: Charmbracelet lipgloss + huh adoption
- `internal/doctor/`: prerequisite check engine (5 checks)
- `internal/setup/`: first-time setup engine
- `internal/status/render.go`: lipgloss-styled rendering
- `sol doctor`, `sol init` (flag/interactive/guided), `sol status` (sphere overview)
- PersistentPreRunE bypass for doctor/init (cmd/root.go)
- Dependencies: charmbracelet/lipgloss, charmbracelet/huh
```

---

## Guidelines

- The integration tests verify the operator-facing behavior end to end.
  They use the compiled binary, not internal function calls.
- JSON output format must be backward compatible. `sol status <world>
  --json` should produce the same structure as before Arc 2.
- The acceptance verification is a script-style checklist. Run each
  command and verify the expected output. All checks must pass.
- Do NOT modify any Go source code in this prompt — only test files,
  documentation, and verification.
- If any tests fail, fix the underlying code (in the appropriate
  internal package or cmd file) before proceeding. The acceptance
  sweep must be clean.
- Commit after verification passes with message:
  `docs: Arc 2 integration tests, arc roadmap, and CLAUDE.md updates`

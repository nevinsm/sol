# Arc 0, Prompt 1: Module, Binary, and Environment Rename

## Context

We are renaming the `gt` system to `sol` — a full rebrand from industrial/Gastown naming to a space-faring theme. This is the first of 4 prompts that together constitute Arc 0. Each prompt leaves the codebase in a compilable, test-passing state.

This prompt handles the foundational rename: Go module path, binary name, environment variables, and config. These changes touch every `.go` file (imports) but are mechanically simple.

Read `docs/naming.md` for the full naming glossary.

## What To Change

### 1. Go Module

**File:** `go.mod`

Change the module declaration:
```
module github.com/nevinsm/gt  →  module github.com/nevinsm/sol
```

Then update **every** import path across every `.go` file in the codebase:
```
"github.com/nevinsm/gt/..."  →  "github.com/nevinsm/sol/..."
```

Use `find . -name '*.go' -exec sed -i 's|github.com/nevinsm/gt/|github.com/nevinsm/sol/|g' {} +` or similar to do this in one pass.

### 2. Binary Name

**File:** `Makefile`

- `bin/gt` → `bin/sol` (build target, line 8)
- `/usr/local/bin/gt` → `/usr/local/bin/sol` (install target, line 61)
- `GT_TEST_HOME := /tmp/gt-test` → `SOL_TEST_HOME := /tmp/sol-test`
- `GT_TEST_RIG` → `SOL_TEST_RIG` (keep value `myrig` for now; rig→world rename is prompt 2)
- `GT_TEST_AGENT` → `SOL_TEST_AGENT`
- All references to these variables throughout the Makefile (`$(GT_TEST_HOME)` → `$(SOL_TEST_HOME)`, etc.)
- Session name pattern: `gt-$$RIG-$$AGENT` → `sol-$$RIG-$$AGENT`
- `bin/gt` → `bin/sol` in all commands
- Comment references to `GT_HOME` → `SOL_HOME`
- Branch cleanup: `polecat/$(GT_TEST_AGENT)/` → `outpost/$(SOL_TEST_AGENT)/` (branch naming convention change)

### 3. Root Command

**File:** `cmd/root.go`

- `Use: "gt"` → `Use: "sol"`

### 4. Environment Variables

**File:** `internal/config/config.go`

- `os.Getenv("GT_HOME")` → `os.Getenv("SOL_HOME")`
- `filepath.Join(os.TempDir(), "gt")` → `filepath.Join(os.TempDir(), "sol")`
- `filepath.Join(home, "gt")` → `filepath.Join(home, "sol")`
- Comment: `// Home returns the GT_HOME directory` → `// Home returns the SOL_HOME directory`
- Comment: `// StoreDir returns the path to $GT_HOME/.store/` → `// StoreDir returns the path to $SOL_HOME/.store/`
- Comment: `// RuntimeDir returns the path to $GT_HOME/.runtime/` → `// RuntimeDir returns the path to $SOL_HOME/.runtime/`
- Comment: `// RigDir returns the path to $GT_HOME/{rig}/` → update (keep "rig" param name for now; rig→world rename is prompt 2)

**File:** `cmd/done.go`

- `os.Getenv("GT_RIG")` → `os.Getenv("SOL_WORLD")`
- `os.Getenv("GT_AGENT")` → `os.Getenv("SOL_AGENT")`
- Error message: `"--rig is required (or set GT_RIG env var)"` → `"--rig is required (or set SOL_WORLD env var)"` (keep `--rig` flag name for now; flag renames are prompt 2)
- Error message: `"--agent is required (or set GT_AGENT env var)"` → `"--agent is required (or set SOL_AGENT env var)"`
- Variable names `doneRig`/`doneAgent` can stay (internal, not user-facing)
- Flag help: `"rig name (defaults to GT_RIG env)"` → `"rig name (defaults to SOL_WORLD env)"`
- Flag help: `"agent name (defaults to GT_AGENT env)"` → `"agent name (defaults to SOL_AGENT env)"`

### 5. Test Helpers

**File:** `test/integration/helpers_test.go`

- `t.Setenv("GT_HOME", gtHome)` → `t.Setenv("SOL_HOME", gtHome)`
- Session cleanup: `strings.HasPrefix(name, "gt-")` → `strings.HasPrefix(name, "sol-")`
- Variable naming: `gtHome` can stay (it's a local variable, not user-facing)
- Comment: `// setupTestEnv creates an isolated test environment with temp GT_HOME` → update to reference `SOL_HOME`

### 6. All Other Test Files

Search for and update in all files under `test/integration/`:
- `"GT_HOME"` → `"SOL_HOME"` (env var string literals)
- `"bin/gt"` → `"bin/sol"` (binary path in exec.Command calls)
- Any hardcoded `"gt-"` session name prefixes → `"sol-"` (only in session name context, NOT in work item IDs yet — those are prompt 2)

### 7. main.go

**File:** `main.go`

- Update import: `"github.com/nevinsm/gt/cmd"` → `"github.com/nevinsm/sol/cmd"` (handled by the global import rename)

## What NOT To Change (Yet)

These are handled in later prompts:
- `--rig` flags and positional args (prompt 2)
- `store.OpenRig`, `store.OpenTown` function names (prompt 2)
- `.hook` files, `polecats/` directories (prompt 2)
- Session name format string `"gt-%s-%s"` in `dispatch.go` (prompt 2 — but DO change the `"gt-"` prefix in test cleanup)
- Package directory names (`internal/hook/`, `internal/supervisor/`, etc.) (prompt 3)
- Command names (`sling`, `done`, `supervisor`, etc.) (prompt 3)
- Documentation files (prompt 4)

## Acceptance Criteria

```bash
make build              # produces bin/sol
make test               # all unit tests pass
grep -rn '"GT_HOME"' --include='*.go' .   # no hits
grep -rn '"GT_RIG"' --include='*.go' .    # no hits
grep -rn '"GT_AGENT"' --include='*.go' .  # no hits
grep -rn 'nevinsm/gt/' --include='*.go' . # no hits
grep -rn 'bin/gt' Makefile                # no hits
bin/sol --help                            # shows "sol" as root command
```

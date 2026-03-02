# Prompt 06: Arc 1 Review-5 — Test Infrastructure

You are fixing test infrastructure issues found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 05 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `test/integration/helpers_test.go` — setupTestEnv, openStores, createSourceRepo
- `test/integration/cli_test.go` — gtBin, runGT
- `test/integration/config_consumer_test.go` — setupGitRepo (duplicate helper)
- `test/integration/loop4_test.go` — TestClaudeMDWithWorkflow, TestClaudeMDWithoutWorkflow (unit tests in integration dir)
- `Makefile` — test targets

---

## Task 1: Fix setupGitRepo missing git user config

**File:** `test/integration/config_consumer_test.go`, `setupGitRepo` function (around line 201)

This helper creates a git repo and commits without configuring `user.email` / `user.name`. In clean CI environments without global git config, `git commit` fails.

**Fix:** Add git user config after `git init`:

```go
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %s: %v", out, err)
	}
	// Configure git user — required in environments without global git config.
	for _, args := range [][]string{
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd = exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %s: %v", args[0], out, err)
		}
	}
	cmd = exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "initial")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}
	return dir
}
```

---

## Task 2: Fix stale binary detection in gtBin

**File:** `test/integration/cli_test.go`, `gtBin` function (around line 13)

The function only checks if the binary exists, not whether it's stale. During development, modifying source and re-running tests uses the old binary.

**Fix:** Check the binary's modification time against the newest `.go` source file:

```go
func gtBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(projectRoot(t), "bin", "sol")

	needsBuild := false
	binInfo, err := os.Stat(bin)
	if os.IsNotExist(err) {
		needsBuild = true
	} else if err == nil {
		// Check if any Go source file is newer than the binary.
		goModInfo, modErr := os.Stat(filepath.Join(projectRoot(t), "go.mod"))
		if modErr == nil && goModInfo.ModTime().After(binInfo.ModTime()) {
			needsBuild = true
		}
	}

	if needsBuild {
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = projectRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build sol binary: %s: %v", out, err)
		}
	}
	return bin
}
```

This checks `go.mod` as a proxy for source changes. It's not perfect (doesn't catch all `.go` file changes) but catches dependency updates and is much better than never rebuilding. A full file walk would be too slow. The alternative is to always rebuild, but that adds latency to every test run.

Actually, the simplest and most correct approach is to always rebuild since `go build` is incremental and fast when nothing changed:

```go
func gtBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(projectRoot(t), "bin", "sol")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build sol binary: %s: %v", out, err)
	}
	return bin
}
```

Use this simpler version. Go's build cache makes incremental builds fast (~200ms when nothing changed).

However, this runs the build for every test that calls `gtBin`. To avoid N redundant builds, use `sync.Once`:

```go
var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

func gtBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		builtBin = filepath.Join(projectRoot(t), "bin", "sol")
		cmd := exec.Command("go", "build", "-o", builtBin, ".")
		cmd.Dir = projectRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build sol binary: %s: %v", out, err)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}
```

Add `"sync"` and `"fmt"` to imports.

---

## Task 3: Move unit tests out of integration directory

**File:** `test/integration/loop4_test.go`

`TestClaudeMDWithWorkflow` and `TestClaudeMDWithoutWorkflow` (around lines 898-948) are pure unit tests — they call `protocol.GenerateClaudeMD()` with no I/O, no store, no filesystem. They don't belong in the integration test directory.

**Fix:** Move these two test functions to `internal/protocol/claudemd_test.go`. If that file doesn't exist, create it. If it exists, append the tests.

The tests import `protocol` directly, so in the new location they'll be in the `protocol` package (or `protocol_test` package). Adjust the package declaration and remove the `protocol.` prefix from function calls:

```go
package protocol_test

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
)

func TestClaudeMDWithWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: true,
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should contain 'sol workflow current'")
	}
	if !strings.Contains(content, "sol workflow advance") {
		t.Error("CLAUDE.md should contain 'sol workflow advance'")
	}
	if !strings.Contains(content, "sol workflow status") {
		t.Error("CLAUDE.md should contain 'sol workflow status'")
	}
	if !strings.Contains(content, "Repeat from step 1") {
		t.Error("CLAUDE.md should contain workflow protocol")
	}
}

func TestClaudeMDWithoutWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: false,
	}

	content := protocol.GenerateClaudeMD(ctx)

	if strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should not contain workflow commands without workflow")
	}
	if !strings.Contains(content, "sol resolve") {
		t.Error("CLAUDE.md should contain 'sol resolve'")
	}
}
```

Delete the original functions from `loop4_test.go` (the `// --- CLAUDE.md Tests ---` section).

---

## Task 4: Check error returns in critical test setup paths

Scan the integration tests for swallowed errors in test setup. Fix the most common patterns. The goal is not to fix every instance (there are ~40+), but to establish the correct pattern and fix the highest-risk ones.

**Files:** `test/integration/loop0_test.go`, `test/integration/loop5_test.go`

Search for patterns like:
- `sphereStore.CreateAgent(` without error check
- `worldStore.CreateWorkItem(` with `_, _` or `_,` discard
- `worldStore.AddDependency(` without error check
- `sphereStore.CreateCaravan(` with `_, _` discard
- `sphereStore.AddCaravanItem(` without error check

Fix each by capturing and checking the error:

```go
// Before:
sphereStore.CreateAgent("TestBot", "ember", "agent")

// After:
if _, err := sphereStore.CreateAgent("TestBot", "ember", "agent"); err != nil {
    t.Fatalf("CreateAgent: %v", err)
}
```

```go
// Before:
idA, _ := worldStore.CreateWorkItem("Task A", "No deps", "operator", 2, nil)

// After:
idA, err := worldStore.CreateWorkItem("Task A", "No deps", "operator", 2, nil)
if err != nil {
    t.Fatalf("CreateWorkItem: %v", err)
}
```

Focus on these files (highest impact):
- `test/integration/loop0_test.go` — all store setup calls
- `test/integration/loop5_test.go` — all store setup calls
- `test/integration/loop3_test.go` — any raw `DB().Exec()` calls

For `DB().Exec()` calls, check the error:

```go
// Before:
worldStore.DB().Exec(`INSERT INTO ...`, args...)

// After:
if _, err := worldStore.DB().Exec(`INSERT INTO ...`, args...); err != nil {
    t.Fatalf("Exec: %v", err)
}
```

---

## Task 5: Update Makefile with test-integration target

**File:** `Makefile`

Add a dedicated `test-integration` target and update the phony list:

```makefile
.PHONY: build test test-short test-integration test-e2e install clean

# ... existing targets ...

test-integration:
	go test -race -run "Test" -count=1 ./test/integration/
```

This gives operators a clear way to run only integration tests.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- `make test-short` skips integration tests
- `make test-integration` runs only integration tests
- Moved tests run in their new location: `go test -v -run "TestClaudeMD" ./internal/protocol/`
- Verify `setupGitRepo` works without global git config: temporarily unset `HOME` and run the config consumer tests

## Commit

```
fix(test): arc 1 review-5 — git config fix, stale binary detection, error checking, test relocation
```

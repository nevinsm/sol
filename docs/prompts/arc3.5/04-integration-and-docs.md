# Prompt 04: Arc 3.5 — Integration Tests and Documentation

**Working directory:** ~/gt-src/
**Prerequisite:** Arc 3.5 prompt 03 complete, `make build && make test` passing

## Context

Read `CLAUDE.md` for project conventions.
Read `docs/decisions/0014-managed-world-repository.md` for the ADR.
Read `test/integration/helpers_test.go` for test helper patterns.
Read `test/integration/init_test.go` for init test patterns.
Read `test/integration/arc3_test.go` for Arc 3 integration test patterns.
Read `internal/setup/setup.go` for `CloneRepo`.
Read `internal/config/config.go` for `RepoPath`.
Read `cmd/world.go` for `worldSyncCmd`.
Read `docs/arc-roadmap.md` for arc documentation patterns.

## Task 1: Integration Tests for Managed Clone

Create `test/integration/arc3_5_test.go`:

### `TestInitWithRemoteURL`

Test that `sol init` works with a bare repo path (simulates remote URL):

```go
func TestInitWithRemoteURL(t *testing.T) {
    solHome := t.TempDir()

    // Create a bare repo (simulates a remote URL).
    remoteDir := filepath.Join(t.TempDir(), "remote.git")
    runGit(t, "", "init", "--bare", remoteDir)

    // Push an initial commit via a temp clone.
    tmpClone := t.TempDir()
    runGit(t, tmpClone, "clone", remoteDir, ".")
    writeTestFile(t, filepath.Join(tmpClone, "README.md"), "hello")
    runGit(t, tmpClone, "add", ".")
    runGit(t, tmpClone, "commit", "-m", "init")
    runGit(t, tmpClone, "push", "origin", "main")

    // Init with "URL" (bare repo path).
    out, err := runGT(t, solHome, "init", "--name=myworld",
        "--source-repo="+remoteDir, "--skip-checks")
    if err != nil {
        t.Fatalf("init failed: %v\n%s", err, out)
    }

    // Verify managed clone exists.
    repoPath := filepath.Join(solHome, "myworld", "repo")
    if _, err := os.Stat(repoPath); err != nil {
        t.Fatalf("managed repo not created: %v", err)
    }

    // Verify it's a valid git repo with the commit.
    logOut := runGitOutput(t, repoPath, "log", "--oneline")
    if !strings.Contains(logOut, "init") {
        t.Error("managed repo missing initial commit")
    }

    // Verify origin points to remote.
    originOut := runGitOutput(t, repoPath, "remote", "get-url", "origin")
    if strings.TrimSpace(originOut) != remoteDir {
        t.Errorf("origin = %q, want %q", strings.TrimSpace(originOut), remoteDir)
    }
}
```

### `TestInitWithLocalPathAdoptsUpstream`

Test that cloning from a local path adopts its upstream:

```go
func TestInitWithLocalPathAdoptsUpstream(t *testing.T) {
    solHome := t.TempDir()

    // Create remote → local clone chain.
    remoteDir := filepath.Join(t.TempDir(), "remote.git")
    runGit(t, "", "init", "--bare", remoteDir)

    localDir := t.TempDir()
    runGit(t, localDir, "clone", remoteDir, ".")
    writeTestFile(t, filepath.Join(localDir, "README.md"), "hello")
    runGit(t, localDir, "add", ".")
    runGit(t, localDir, "commit", "-m", "init")
    runGit(t, localDir, "push", "origin", "main")

    // Init with local path.
    out, err := runGT(t, solHome, "init", "--name=myworld",
        "--source-repo="+localDir, "--skip-checks")
    if err != nil {
        t.Fatalf("init failed: %v\n%s", err, out)
    }

    // Verify managed clone's origin is the REMOTE, not the local path.
    repoPath := filepath.Join(solHome, "myworld", "repo")
    originOut := runGitOutput(t, repoPath, "remote", "get-url", "origin")
    origin := strings.TrimSpace(originOut)

    if origin == localDir {
        t.Errorf("origin should be upstream (%s), got local path (%s)", remoteDir, localDir)
    }
    if origin != remoteDir {
        t.Errorf("origin = %q, want %q", origin, remoteDir)
    }
}
```

### `TestWorldSyncFetchesLatest`

Test that `sol world sync` pulls new commits:

```go
func TestWorldSyncFetchesLatest(t *testing.T) {
    solHome := t.TempDir()

    // Create remote with initial commit.
    remoteDir := filepath.Join(t.TempDir(), "remote.git")
    runGit(t, "", "init", "--bare", remoteDir)

    tmpClone := t.TempDir()
    runGit(t, tmpClone, "clone", remoteDir, ".")
    writeTestFile(t, filepath.Join(tmpClone, "file1.txt"), "v1")
    runGit(t, tmpClone, "add", ".")
    runGit(t, tmpClone, "commit", "-m", "first")
    runGit(t, tmpClone, "push", "origin", "main")

    // Init world.
    out, err := runGT(t, solHome, "init", "--name=myworld",
        "--source-repo="+remoteDir, "--skip-checks")
    if err != nil {
        t.Fatalf("init failed: %v\n%s", err, out)
    }

    // Push a new commit to remote.
    writeTestFile(t, filepath.Join(tmpClone, "file2.txt"), "v2")
    runGit(t, tmpClone, "add", ".")
    runGit(t, tmpClone, "commit", "-m", "second")
    runGit(t, tmpClone, "push", "origin", "main")

    // Sync.
    out, err = runGT(t, solHome, "world", "sync", "myworld")
    if err != nil {
        t.Fatalf("world sync failed: %v\n%s", err, out)
    }

    // Verify managed repo has the new commit.
    repoPath := filepath.Join(solHome, "myworld", "repo")
    logOut := runGitOutput(t, repoPath, "log", "--oneline")
    if !strings.Contains(logOut, "second") {
        t.Error("managed repo missing second commit after sync")
    }

    // Verify file2.txt exists in working tree.
    if _, err := os.Stat(filepath.Join(repoPath, "file2.txt")); err != nil {
        t.Error("file2.txt not present in managed repo after sync")
    }
}
```

### `TestWorldSyncLateClone`

Test the late-initialization path where sync creates the clone:

```go
func TestWorldSyncLateClone(t *testing.T) {
    solHome := t.TempDir()

    // Create remote.
    remoteDir := filepath.Join(t.TempDir(), "remote.git")
    runGit(t, "", "init", "--bare", remoteDir)
    tmpClone := t.TempDir()
    runGit(t, tmpClone, "clone", remoteDir, ".")
    writeTestFile(t, filepath.Join(tmpClone, "README.md"), "hello")
    runGit(t, tmpClone, "add", ".")
    runGit(t, tmpClone, "commit", "-m", "init")
    runGit(t, tmpClone, "push", "origin", "main")

    // Init WITHOUT source-repo.
    out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
    if err != nil {
        t.Fatalf("init failed: %v\n%s", err, out)
    }

    // Verify no managed repo yet.
    repoPath := filepath.Join(solHome, "myworld", "repo")
    if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
        t.Fatal("repo should not exist yet")
    }

    // Manually set source_repo in world.toml.
    tomlPath := filepath.Join(solHome, "myworld", "world.toml")
    data, err := os.ReadFile(tomlPath)
    if err != nil {
        t.Fatal(err)
    }
    content := strings.Replace(string(data),
        `source_repo = ""`,
        fmt.Sprintf(`source_repo = "%s"`, remoteDir),
        1)
    if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }

    // Sync — should clone.
    out, err = runGT(t, solHome, "world", "sync", "myworld")
    if err != nil {
        t.Fatalf("world sync failed: %v\n%s", err, out)
    }

    // Verify clone was created.
    if _, err := os.Stat(repoPath); err != nil {
        t.Fatalf("repo not created by sync: %v", err)
    }

    logOut := runGitOutput(t, repoPath, "log", "--oneline")
    if !strings.Contains(logOut, "init") {
        t.Error("managed repo missing initial commit")
    }
}
```

### `TestCastUsesManagedRepo`

Test that cast creates worktrees from the managed clone:

```go
func TestCastUsesManagedRepo(t *testing.T) {
    gtHome, sourceRepo := setupTestEnvWithRepo(t)
    setupWorld(t, gtHome, "myworld", sourceRepo)

    // Create a work item.
    out, err := runGT(t, gtHome, "store", "create",
        "--world=myworld", "--title=test task", "--description=test")
    if err != nil {
        t.Fatalf("store create failed: %v\n%s", err, out)
    }

    // Extract work item ID.
    workItemID := extractWorkItemID(t, out)

    // Cast.
    out, err = runGT(t, gtHome, "cast", workItemID, "myworld")
    if err != nil {
        t.Fatalf("cast failed: %v\n%s", err, out)
    }

    // Verify the outpost worktree was created.
    // The worktree should be linked to the managed repo, not the source repo.
    repoPath := filepath.Join(gtHome, "myworld", "repo")
    worktreeListOut := runGitOutput(t, repoPath, "worktree", "list")
    if !strings.Contains(worktreeListOut, "outposts") {
        t.Error("managed repo should list the outpost worktree")
    }
}
```

### Test helpers

Add to `test/integration/helpers_test.go` (or the new test file) if not already
present:

```go
func runGit(t *testing.T, dir string, args ...string) {
    t.Helper()
    cmd := exec.Command("git", args...)
    if dir != "" {
        cmd.Dir = dir
    }
    cmd.Env = append(os.Environ(),
        "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
        "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
    )
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("git %v failed in %s: %s: %v", args, dir, out, err)
    }
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
    t.Helper()
    cmd := exec.Command("git", args...)
    cmd.Dir = dir
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("git %v failed in %s: %s: %v", args, dir, out, err)
    }
    return string(out)
}

func writeTestFile(t *testing.T, path, content string) {
    t.Helper()
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
}
```

Check for naming conflicts with existing helpers — use different names if needed.

If `setupTestEnvWithRepo` doesn't exist, create a helper that:
1. Creates a temp dir for SOL_HOME
2. Creates a git repo with an initial commit
3. Returns (solHome, sourceRepoPath)

If `setupWorld` doesn't exist, create a helper that runs
`sol world init <name> --source-repo=<path>` and fails the test on error.

## Task 2: Update Existing Integration Tests

### `test/integration/init_test.go`

`TestInitSourceRepoValidation` tests that nonexistent paths and non-directory
paths are rejected. With the managed clone change, the error now comes from
`git clone` instead of `os.Stat`. Update:

- The test should still verify `err != nil` for both cases
- The error message content may have changed — update any substring assertions
  to match the new git clone error messages, or just check `err != nil` without
  asserting on message content

### `test/integration/world_lifecycle_test.go`

Many tests use `--source-repo=/tmp` as a valid local path. With managed clones,
this will attempt to clone `/tmp` as a git repo, which will fail (it's not a
git repo).

Update these tests to use a real git repo:

- Add a helper that creates a temporary git repo with an initial commit
- Replace `--source-repo=/tmp` with `--source-repo=<tempRepo>` throughout

If many tests need this, add a shared helper in `helpers_test.go`:

```go
// createTestRepo creates a temporary git repo with one commit and returns its path.
func createTestRepo(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    runGit(t, dir, "init")
    runGit(t, dir, "commit", "--allow-empty", "-m", "init")
    return dir
}
```

Then replace all `--source-repo=/tmp` with `--source-repo="+createTestRepo(t)`.

### `test/integration/hard_gate_test.go`

Same pattern — uses `--source-repo=/tmp`. Replace with `createTestRepo(t)`.

### `test/integration/config_consumer_test.go`

These tests already use a real git repo (the `sourceRepo` variable from helpers).
Verify they still pass — the managed clone should be created at
`$SOL_HOME/{world}/repo/`.

### `test/integration/doctor_test.go`

Uses `--source-repo=/tmp`. Replace with `createTestRepo(t)`.

## Task 3: Update Documentation

### `docs/arc-roadmap.md`

Add Arc 3.5 entry after Arc 3:

```markdown
### Arc 3.5 — Managed World Repository
**Status:** Complete
**ADR:** 0014 — Managed World Repository

- Worlds accept remote URLs (HTTPS, SSH) as `source_repo`
- `sol init --source-repo=git@github.com:org/repo.git` clones to `$SOL_HOME/{world}/repo/`
- All worktree operations (cast, envoy, forge) use the managed clone
- Governor mirror eliminated — reads from managed clone
- `sol world sync <world>` fetches latest from origin
- Local path sources adopt upstream remote for correct push semantics
```

### `CLAUDE.md`

In the Key Concepts section, add:

```markdown
- **Managed Repo**: Clone at $SOL_HOME/{world}/repo/ — source for all worktrees
```

Update the Build & Test or Components section if there are references to
`source_repo` being a local path.

### `README.md`

Update the init example (line 37) from:
```
sol world init myworld --source-repo=/path/to/your/repo
```
To:
```
sol world init myworld --source-repo=git@github.com:org/your-repo.git
```

Or show both:
```
sol world init myworld --source-repo=git@github.com:org/your-repo.git
sol world init myworld --source-repo=/path/to/local/repo
```

## Verification

- `make build && make test` passes
- All integration tests pass (especially the `--source-repo=/tmp` replacements)
- New tests exercise:
  - URL-based init
  - Local path with upstream adoption
  - World sync fetch
  - World sync late clone
  - Cast using managed repo

## Guidelines

- Test helpers should be reusable — put them in `helpers_test.go` if they'll be
  used across test files
- The `createTestRepo` helper should be minimal — one empty commit is enough
- Don't over-test the git operations — trust git, test the sol integration layer
- Keep test names descriptive: `TestWorldSync...`, `TestInitWith...`

## Commit

`test(world): integration tests for managed repo, sync, and URL init`

# Prompt 01: Arc 3.5 — Managed Clone and Init

**Working directory:** ~/gt-src/
**Prerequisite:** Arc 3 complete, `make build && make test` passing

## Context

Read `CLAUDE.md` for project conventions.
Read `docs/decisions/0014-managed-world-repository.md` for the ADR.
Read `internal/config/config.go` for path helpers (`Home`, `WorldDir`, etc.).
Read `internal/config/world_config.go` for `WorldConfig` struct and `LoadWorldConfig`.
Read `internal/setup/setup.go` for the `setup.Run()` flow.
Read `cmd/init.go` for the three init modes (flag, interactive, guided).
Read `cmd/world.go` for `worldInitCmd`.

## Task 1: Add `config.RepoPath()` Helper

In `internal/config/config.go`, add a new path function:

```go
// RepoPath returns the path to the managed git clone for a world.
func RepoPath(world string) string {
    return filepath.Join(WorldDir(world), "repo")
}
```

## Task 2: Add `CloneRepo` to Setup Package

Create a new function in `internal/setup/setup.go` that handles cloning a source
repo (URL or local path) into the managed clone location. This is the core of the
arc.

```go
// CloneRepo clones the given source (URL or local path) into the managed
// repo directory at config.RepoPath(world). If the source is a local path
// and has an upstream origin remote, the managed clone's origin is set to
// that upstream URL so pushes go directly to the real remote.
func CloneRepo(world, source string) error {
```

Implementation:

1. Compute `repoPath := config.RepoPath(world)`
2. If `repoPath` already exists, return `fmt.Errorf("managed repo already exists for world %q", world)`
3. Create parent directory: `os.MkdirAll(filepath.Dir(repoPath), 0o755)`
4. Run `git clone <source> <repoPath>`:
   ```go
   cmd := exec.Command("git", "clone", source, repoPath)
   if out, err := cmd.CombinedOutput(); err != nil {
       return fmt.Errorf("failed to clone source repo for world %q: %s: %w",
           world, strings.TrimSpace(string(out)), err)
   }
   ```
5. Adopt upstream origin for local paths. Detect if `source` is a local path
   (not a URL) by checking if `os.Stat(source)` succeeds:
   ```go
   if info, err := os.Stat(source); err == nil && info.IsDir() {
       // Source is a local path — check if it has an upstream origin.
       upstreamCmd := exec.Command("git", "-C", source, "remote", "get-url", "origin")
       if upstreamOut, err := upstreamCmd.Output(); err == nil {
           upstream := strings.TrimSpace(string(upstreamOut))
           if upstream != "" && upstream != source {
               // Set managed clone's origin to the real upstream.
               setCmd := exec.Command("git", "-C", repoPath, "remote", "set-url", "origin", upstream)
               if out, err := setCmd.CombinedOutput(); err != nil {
                   return fmt.Errorf("failed to set upstream origin for world %q: %s: %w",
                       world, strings.TrimSpace(string(out)), err)
               }
           }
       }
   }
   ```
6. Return nil.

Import `os/exec` and `strings` in setup.go.

## Task 3: Integrate Clone into `setup.Run()`

In `internal/setup/setup.go`, `Run()`, after step 4 (create world directory +
outposts/), add a clone step if source repo is provided:

```go
// 4b. Clone source repo into managed repo directory.
if p.SourceRepo != "" {
    if err := CloneRepo(p.WorldName, p.SourceRepo); err != nil {
        return nil, fmt.Errorf("failed to clone source repo: %w", err)
    }
}
```

Insert this between step 4 (line 93–95) and step 5 (line 97–102).

## Task 4: Remove `os.Stat` Validation from Init Commands

The `os.Stat` + `IsDir` validation in `cmd/init.go` rejects URLs. Remove it
from both flag mode and interactive mode. The `git clone` in `CloneRepo` is the
validation now — if the source is invalid, clone fails with a clear git error.

### Flag mode (`runFlagInit`, line 68–77)

Remove the entire validation block:
```go
// DELETE these lines:
if initSourceRepo != "" {
    info, err := os.Stat(initSourceRepo)
    if err != nil {
        return fmt.Errorf("source repo path %q: %w", initSourceRepo, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("source repo path %q is not a directory", initSourceRepo)
    }
}
```

### Interactive mode (`runInteractiveInit`, line 143–152)

Remove the same validation block:
```go
// DELETE these lines:
if sourceRepo != "" {
    info, err := os.Stat(sourceRepo)
    if err != nil {
        return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
    }
}
```

### Interactive form description

Update the source repo input description (line 124):

Change:
```go
Description("Path to your project's git repo (optional)").
Placeholder("/path/to/repo").
```
To:
```go
Description("Git URL or local path to your project's repo (optional)").
Placeholder("git@github.com:org/repo.git").
```

### Init help text (line 32)

Change:
```go
  Flag mode:        sol init --name=myworld [--source-repo=/path]
```
To:
```go
  Flag mode:        sol init --name=myworld [--source-repo=<url-or-path>]
```

### Flag description (line 280)

Change:
```go
"path to source git repository"
```
To:
```go
"git URL or local path to source repository"
```

## Task 5: Remove `os.Stat` Validation from `worldInitCmd`

In `cmd/world.go`, `worldInitCmd` (lines 68–76):

Remove the entire validation block:
```go
// DELETE these lines:
if sourceRepo != "" {
    info, err := os.Stat(sourceRepo)
    if err != nil {
        return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
    }
}
```

Add the clone step after the directory creation (line 82), before the database
step:

```go
// Clone source repo into managed repo directory.
if sourceRepo != "" {
    if err := setup.CloneRepo(name, sourceRepo); err != nil {
        return err
    }
}
```

Import `"github.com/nevinsm/sol/internal/setup"` in world.go.

Update the flag description (line 352–353):

Change:
```go
"path to source git repository"
```
To:
```go
"git URL or local path to source repository"
```

## Task 6: Tests

### Unit test: `CloneRepo` in `internal/setup/setup_test.go`

Add `TestCloneRepoFromLocalPath`:

```go
func TestCloneRepoFromLocalPath(t *testing.T) {
    // Create a source git repo with a commit.
    sourceDir := t.TempDir()
    runGit(t, sourceDir, "init")
    runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

    // Set SOL_HOME to temp dir.
    solHome := t.TempDir()
    t.Setenv("SOL_HOME", solHome)

    world := "testworld"
    worldDir := filepath.Join(solHome, world)
    os.MkdirAll(worldDir, 0o755)

    err := CloneRepo(world, sourceDir)
    if err != nil {
        t.Fatalf("CloneRepo failed: %v", err)
    }

    // Verify clone exists and is a git repo.
    repoPath := config.RepoPath(world)
    cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
    if _, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("managed clone is not a git repo: %v", err)
    }
}
```

Add `TestCloneRepoAdoptsUpstream`:

```go
func TestCloneRepoAdoptsUpstream(t *testing.T) {
    // Create a "remote" repo.
    remoteDir := t.TempDir()
    runGit(t, remoteDir, "init", "--bare")

    // Create a local repo cloned from the remote.
    localDir := t.TempDir()
    runGit(t, localDir, "clone", remoteDir, ".")

    // Create initial commit in local and push.
    writeFile(t, filepath.Join(localDir, "README.md"), "hello")
    runGit(t, localDir, "add", ".")
    runGit(t, localDir, "commit", "-m", "init")
    runGit(t, localDir, "push", "origin", "main")

    // Clone from local path.
    solHome := t.TempDir()
    t.Setenv("SOL_HOME", solHome)
    world := "testworld"
    os.MkdirAll(filepath.Join(solHome, world), 0o755)

    err := CloneRepo(world, localDir)
    if err != nil {
        t.Fatalf("CloneRepo failed: %v", err)
    }

    // Verify managed clone's origin points to the remote, not the local path.
    repoPath := config.RepoPath(world)
    cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
    out, err := cmd.Output()
    if err != nil {
        t.Fatalf("failed to get origin URL: %v", err)
    }
    origin := strings.TrimSpace(string(out))
    if origin == localDir {
        t.Errorf("managed clone origin should be upstream (%s), got local path (%s)", remoteDir, origin)
    }
    if origin != remoteDir {
        t.Errorf("managed clone origin = %q, want %q", origin, remoteDir)
    }
}
```

Add `TestCloneRepoFromURL` (simulated with a bare repo as "remote"):

```go
func TestCloneRepoFromURL(t *testing.T) {
    // Create a bare repo (simulates a remote URL — git clone works with paths).
    remoteDir := t.TempDir()
    runGit(t, remoteDir, "init", "--bare")

    // Push an initial commit via a temp clone.
    tmpClone := t.TempDir()
    runGit(t, tmpClone, "clone", remoteDir, ".")
    writeFile(t, filepath.Join(tmpClone, "README.md"), "hello")
    runGit(t, tmpClone, "add", ".")
    runGit(t, tmpClone, "commit", "-m", "init")
    runGit(t, tmpClone, "push", "origin", "main")

    // Clone using the bare path (acts like a URL — os.Stat won't IsDir true for bare).
    solHome := t.TempDir()
    t.Setenv("SOL_HOME", solHome)
    world := "testworld"
    os.MkdirAll(filepath.Join(solHome, world), 0o755)

    err := CloneRepo(world, remoteDir)
    if err != nil {
        t.Fatalf("CloneRepo failed: %v", err)
    }

    // Verify clone has the commit.
    repoPath := config.RepoPath(world)
    cmd := exec.Command("git", "-C", repoPath, "log", "--oneline")
    out, err := cmd.Output()
    if err != nil {
        t.Fatalf("git log failed: %v", err)
    }
    if !strings.Contains(string(out), "init") {
        t.Error("managed clone missing initial commit")
    }
}
```

Add `TestCloneRepoAlreadyExists`:

```go
func TestCloneRepoAlreadyExists(t *testing.T) {
    solHome := t.TempDir()
    t.Setenv("SOL_HOME", solHome)
    world := "testworld"
    repoPath := config.RepoPath(world)
    os.MkdirAll(repoPath, 0o755)

    err := CloneRepo(world, "/tmp/fake")
    if err == nil {
        t.Fatal("expected error when repo already exists")
    }
    if !strings.Contains(err.Error(), "already exists") {
        t.Errorf("unexpected error: %v", err)
    }
}
```

Add helper functions `runGit` and `writeFile` at the top of the test file if they
don't exist:

```go
func runGit(t *testing.T, dir string, args ...string) {
    t.Helper()
    cmd := exec.Command("git", args...)
    cmd.Dir = dir
    cmd.Env = append(os.Environ(),
        "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
        "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
    )
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("git %v failed: %s: %v", args, out, err)
    }
}

func writeFile(t *testing.T, path, content string) {
    t.Helper()
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
}
```

### Update cmd/init_test.go

The existing `TestInitSourceRepoValidation` tests check for `os.Stat` rejection
of nonexistent paths. With the change, a nonexistent path will fail at `git clone`
instead. Update the test assertions:

- `TestInitSourceRepoValidation/nonexistent_path`: The error message changes from
  "source repo path ... no such file" to a git clone error. Just verify `err != nil`.
- `TestInitSourceRepoValidation/path_is_file`: Same — error comes from git clone
  now, not `os.Stat`. Just verify `err != nil`.

## Verification

- `make build && make test` passes
- Test URL acceptance:
  ```bash
  # Create a test bare repo
  git init --bare /tmp/test-remote.git
  cd /tmp && git clone test-remote.git test-clone
  cd test-clone && git commit --allow-empty -m "init" && git push origin main
  cd ~

  # Init with a "URL" (bare repo path simulates URL behavior)
  SOL_HOME=/tmp/sol-test bin/sol init --name=urlworld --source-repo=/tmp/test-remote.git --skip-checks

  # Verify clone exists
  ls /tmp/sol-test/urlworld/repo/
  git -C /tmp/sol-test/urlworld/repo/ remote -v

  # Clean up
  rm -rf /tmp/sol-test /tmp/test-remote.git /tmp/test-clone
  ```
- Test local path with upstream adoption:
  ```bash
  SOL_HOME=/tmp/sol-test bin/sol init --name=localworld --source-repo=/path/to/local/repo --skip-checks
  git -C /tmp/sol-test/localworld/repo/ remote get-url origin
  # Should show the upstream URL, not the local path
  ```

## Guidelines

- `CloneRepo` uses `git clone` which handles HTTPS, SSH, and local paths natively
- The upstream adoption (step 5) is best-effort — if the local repo has no remote,
  the managed clone's origin stays as the local path
- Do not change `WorldConfig.SourceRepo` semantics — it still stores the original
  value. The managed clone path is derived via `config.RepoPath()`
- Do not change any worktree consumers yet — that's prompt 02

## Commit

`feat(world): managed repo clone on init — accept URLs and local paths (ADR-0014)`

# SAST: Concurrency Issues

Run gosec to detect concurrency vulnerabilities, then validate each finding and manually inspect security-sensitive concurrent code.

## Tool Execution

Run gosec filtered to concurrency-related rules:

```bash
gosec -include=G601 -fmt=json -quiet ./...
```

**Rule coverage:**
- **G601**: Implicit memory aliasing of items from a range statement (can cause data races)

Note: gosec's concurrency coverage is limited. The manual review section below is more important than the tool output for this step.

If gosec is not installed, install it:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

## Validation

For each gosec finding:

1. **Read the cited file and line** — confirm the code exists
2. **Check Go version** — G601 was fixed in Go 1.22 (loop variable semantics changed). If the project uses Go 1.22+, most G601 findings are false positives. Check `go.mod` for the Go version.
3. **If pre-1.22 or the aliased pointer escapes**: determine if the aliased value is used concurrently or stored for later use
4. **Assess security impact** — does the race condition affect authentication, authorization, state integrity, or process control?

## Manual Concurrency Review

This is the primary value of this step. Review the codebase for concurrency issues that gosec cannot detect:

### Race Conditions in Security-Sensitive Code

Look for shared mutable state in:
- **Tether management** (`internal/tether/`): concurrent tether creation/deletion, file-based state races
- **Session lifecycle** (`internal/session/`, `internal/dispatch/`): concurrent session start/stop, state transitions
- **Store access** (`internal/store/`): concurrent SQLite operations, transaction isolation
- **Forge pipeline** (`internal/forge/`): concurrent merge operations, state machine transitions
- **Service management** (`internal/service/`): concurrent service start/stop, health check races

For each suspicious pattern:
1. **Identify the shared state** — what variable, file, or database row is accessed concurrently?
2. **Identify the goroutines** — which goroutines access it? Are they created by the same function or different entry points?
3. **Check synchronization** — is there a mutex, channel, or atomic operation protecting the access?
4. **Assess the race window** — how likely is the race in practice? (high-frequency vs. startup-only)
5. **Assess the impact** — what happens if the race fires? State corruption? Security bypass? Crash?

### Specific Patterns to Check

- **Check-then-act on files**: `if fileExists { readFile }` without holding a lock — another goroutine could delete/modify between check and act
- **Map access without synchronization**: Go maps are not goroutine-safe. Any map accessed from multiple goroutines without sync.RWMutex or sync.Map
- **Goroutine leaks in security paths**: goroutines spawned for security operations (auth checks, TLS handshakes) that are never joined — errors may be lost
- **Signal handling races**: concurrent signal handlers modifying shared state during graceful shutdown

### Go Race Detector

If practical, also run the test suite with race detection:
```bash
go test -race -count=1 ./...
```

Report any races detected, especially in packages handling security-sensitive state.

## Output

Write all findings to `review.md` in your writ output directory.

Each finding must include:
1. One-line summary
2. gosec rule ID, "MANUAL", or "RACE-DETECTOR"
3. File path and line range
4. **The actual code** — quote the specific lines showing both sides of the race
5. Shared state identification — what exactly is being raced on?
6. Synchronization assessment — what protection exists (or is missing)?
7. Severity: **CRITICAL** / **HIGH** / **MEDIUM** / **LOW**
8. CWE ID (CWE-362 for race conditions, CWE-667 for improper locking)

## Severity Guide

- **CRITICAL**: Race condition that can bypass authentication/authorization or corrupt security state
- **HIGH**: Race condition on state that gates process execution or data integrity
- **MEDIUM**: Race condition on operational state (health checks, status, metrics) or with narrow window
- **LOW**: Theoretical race with no practical security impact, or G601 in Go 1.22+

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**Include the code.** Every finding must quote the specific lines from the source — both the concurrent accesses, not just one side.

**Practical severity.** A race condition in a test helper is not the same as a race in the forge merge pipeline. Assess based on real-world impact.

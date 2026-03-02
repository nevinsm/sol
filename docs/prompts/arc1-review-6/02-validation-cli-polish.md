# Prompt 02: Arc 1 Review-6 — Agent Name Validation, Context Fix, SilenceUsage Sweep

You are adding defensive validation for agent names, fixing a context propagation bug, and adding `SilenceUsage: true` to all commands that perform I/O.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/config/config.go` — `ValidateWorldName()` as the reference pattern
- `internal/config/config_test.go` — existing validation tests
- `internal/store/agents.go` — `CreateAgent()` function
- `internal/namepool/namepool.go` — `AllocateName()` function
- `cmd/escalate.go` — context propagation bug
- `cmd/forge.go` — example of commands with and without `SilenceUsage`

---

## Task 1: Add `ValidateAgentName()`

**File:** `internal/config/config.go`

World names are validated with `ValidateWorldName()` (regex, length, reserved names). Agent names have no validation at all, despite being used in filesystem paths (`TetherPath`, `HandoffPath`, `WorktreePath`) and tmux session names. Names come from the name pool, but a corrupted or malicious override file could inject path traversal.

Add a `ValidateAgentName()` function with similar rules to `ValidateWorldName()`. Agent names should be alphanumeric with hyphens, underscores, and dots allowed (to support names like "Nova", "Vega-2", etc.):

```go
var validAgentName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)

const maxAgentNameLen = 64

// ValidateAgentName checks that an agent name contains only safe characters.
// Names must start with a letter, contain only [a-zA-Z0-9._-], and be at most 64 chars.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name must not be empty")
	}
	if len(name) > maxAgentNameLen {
		return fmt.Errorf("agent name %q is too long (%d chars, max %d)", name, len(name), maxAgentNameLen)
	}
	if !validAgentName.MatchString(name) {
		return fmt.Errorf("invalid agent name %q: must start with a letter and contain only [a-zA-Z0-9._-]", name)
	}
	return nil
}
```

---

## Task 2: Call `ValidateAgentName` at boundaries

**File:** `internal/store/agents.go`, `CreateAgent` function

Add validation at the top of `CreateAgent`, before any database operations:

```go
func (s *Store) CreateAgent(name, world, role string) (string, error) {
	if err := config.ValidateAgentName(name); err != nil {
		return "", fmt.Errorf("invalid agent: %w", err)
	}
	// ... rest of function unchanged
```

This requires adding `"github.com/nevinsm/sol/internal/config"` to the imports in `agents.go`. Check that this doesn't create a circular import — `config` should not import `store`. If it does create a cycle, define the validation regex and function in a new file `internal/store/validate.go` instead, duplicating the logic.

**File:** `internal/namepool/namepool.go`, `AllocateName` function

Validate each name as it's loaded from the file, not at allocation time (to fail early on bad pool files). Add validation in `parseNames`:

```go
func parseNames(text string) []string {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Silently skip invalid names from override files.
		if !validAgentNameRe.MatchString(line) {
			continue
		}
		names = append(names, line)
	}
	return names
}
```

Since `namepool` shouldn't import `config`, define a local regex:

```go
var validAgentNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)
```

This is intentional duplication — the namepool is a leaf package and the regex is trivial.

---

## Task 3: Fix `escalate.go` context propagation

**File:** `cmd/escalate.go`, line 48

Change:
```go
if err := router.Route(context.Background(), *esc); err != nil {
```

To:
```go
if err := router.Route(cmd.Context(), *esc); err != nil {
```

Remove the `"context"` import if it becomes unused (it will, since `cmd.Context()` comes from cobra).

---

## Task 4: Add `SilenceUsage: true` to all I/O commands

When a command does I/O (opens a database, reads/writes files, runs tmux commands), it should set `SilenceUsage: true` so that cobra doesn't print usage on runtime errors.

Add `SilenceUsage: true` to every command struct literal listed below. Each change is a single line addition inside the `&cobra.Command{}` literal. Add it after the `Args:` line (or after `Short:` if no `Args:`), consistent with existing commands.

**Commands to fix (all have `RunE` and do I/O):**

`cmd/session.go`:
- Line ~42: `sessionStartCmd` (Use: "start")
- Line ~81: `sessionStopCmd` (Use: "stop")
- Line ~103: `sessionListCmd` (Use: "list")
- Line ~173: `sessionCaptureCmd` (Use: "capture")
- Line ~194: `sessionAttachCmd` (Use: "attach")
- Line ~208: `sessionInjectCmd` (Use: "inject")

`cmd/forge.go`:
- Line ~26: `forgeStartCmd` (Use: "start")
- Line ~123: `forgeStopCmd` (Use: "stop")
- Line ~150: `forgeAttachCmd` (Use: "attach")
- Line ~174: `forgeQueueCmd` (Use: "queue")
- Line ~270: `forgeReadyCmd` (Use: "ready")
- Line ~313: `forgeBlockedCmd` (Use: "blocked")
- Line ~356: `forgeClaimCmd` (Use: "claim")
- Line ~408: `forgeReleaseCmd` (Use: "release")
- Line ~484: `forgePushCmd` (Use: "push")
- Line ~511: `forgeMarkMergedCmd` (Use: "mark-merged")
- Line ~543: `forgeMarkFailedCmd` (Use: "mark-failed")
- Line ~575: `forgeCreateResolutionCmd` (Use: "create-resolution")
- Line ~617: `forgeCheckUnblockedCmd` (Use: "check-unblocked")

`cmd/sentinel.go`:
- Line ~24: `sentinelRunCmd` (Use: "run")
- Line ~75: `sentinelStartCmd` (Use: "start")
- Line ~112: `sentinelStopCmd` (Use: "stop")
- Line ~139: `sentinelAttachCmd` (Use: "attach")

`cmd/prefect.go`:
- Line ~24: `prefectRunCmd` (Use: "run")
- Line ~72: `prefectStopCmd` (Use: "stop")

`cmd/workflow.go`:
- Line ~82: `workflowAdvanceCmd` (Use: "advance")
- Line ~128: `workflowStatusCmd` (Use: "status")

Note: `workflowInstantiateCmd` and `workflowCurrentCmd` already have it — verify this.

`cmd/chronicle.go`:
- Line ~23: `chronicleRunCmd` (Use: "run")
- Line ~46: `chronicleStartCmd` (Use: "start")
- Line ~76: `chronicleStopCmd` (Use: "stop")

`cmd/escalation.go`:
- Line ~95: `escalationAckCmd` (Use: "ack")
- Line ~125: `escalationResolveCmd` (Use: "resolve")

`cmd/caravan.go`:
- Line ~85: `caravanAddCmd` (Use: "add")
- Line ~330: `caravanLaunchCmd` (Use: "launch")

`cmd/feed.go`:
- Line ~27: `feedCmd` (Use: "feed")

`cmd/mail.go`:
- Line ~19: `mailSendCmd` (Use: "send")
- Line ~47: `mailInboxCmd` (Use: "inbox")
- Line ~87: `mailReadCmd` (Use: "read")
- Line ~114: `mailAckCmd` (Use: "ack")

`cmd/log_event.go`:
- Line ~20: `logEventCmd` (Use: "log-event")

---

## Task 5: Tests

**File:** `internal/config/config_test.go`

Add tests for agent name validation:

```go
func TestValidateAgentNameEmpty(t *testing.T) {
	err := ValidateAgentName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateAgentNameValid(t *testing.T) {
	valid := []string{"Nova", "Vega", "agent-1", "Toast_v2", "R2.D2"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAgentName(name); err != nil {
				t.Fatalf("expected name %q to be valid, got: %v", name, err)
			}
		})
	}
}

func TestValidateAgentNameInvalid(t *testing.T) {
	invalid := []string{"../evil", "foo/bar", "1starts-digit", ".hidden", " space", "semi;colon"}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAgentName(name); err == nil {
				t.Fatalf("expected error for invalid name %q", name)
			}
		})
	}
}

func TestValidateAgentNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 65)
	if err := ValidateAgentName(long); err == nil {
		t.Fatal("expected error for name exceeding max length")
	}
}
```

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify no circular import between `store` and `config`. If there is one, the agent name validation in `CreateAgent` should use a local validation instead.
- Spot-check a few commands: run `bin/sol session list` in a non-sol directory — should get a runtime error without usage text. Run `bin/sol session list --help` — should still get usage text.

## Commit

```
fix(config,store,cmd): arc 1 review-6 — agent name validation, escalate context, SilenceUsage sweep
```

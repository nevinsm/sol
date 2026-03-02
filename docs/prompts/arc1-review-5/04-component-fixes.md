# Prompt 04: Arc 1 Review-5 — Component Fixes

You are fixing issues in the autonomous components (sentinel, consul, forge, prefect, protocol) found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 03 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/sentinel/sentinel.go` — Register, runAssessment, buildAssessmentPrompt
- `internal/consul/consul.go` — Register, plural function
- `internal/forge/forge.go` — Config struct (GateTimeout field)
- `internal/forge/toolbox.go` — RunGates (GateTimeout parsing)
- `internal/protocol/claudemd.go` — ForgeClaudeMDContext, GenerateForgeClaudeMD
- `internal/protocol/hooks.go` — InstallHooks
- `internal/prefect/prefect.go` — startConsul (backoff tracking)
- `internal/config/world_config.go` — Validate function
- `internal/escalation/mail.go` — MailNotifier

---

## Task 1: Propagate parent context in sentinel runAssessment

**File:** `internal/sentinel/sentinel.go`, `runAssessment` function (around line 332)

The function creates `context.WithTimeout(context.Background(), 30*time.Second)` instead of using the parent context. This means shutdown is blocked up to 30 seconds by an in-flight AI assessment.

**Fix:** Add a `ctx context.Context` parameter to `runAssessment` and use it as the parent:

```go
func (w *Sentinel) runAssessment(ctx context.Context, agent store.Agent, capturedOutput string) (*AssessmentResult, error) {
	prompt := buildAssessmentPrompt(agent, capturedOutput)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// ... rest unchanged
```

Update all callers of `runAssessment` to pass the context. The caller is `assessAgent` — find where it's called and thread the context through. If `assessAgent` doesn't have a context parameter, add one and update its callers (which should be in the `patrol` → `checkProgress` → `assessAgent` chain, ultimately rooted in `Run`'s context).

Thread the context from `Run(ctx)` → `patrol(ctx)` → `checkProgress(ctx, ...)` → `assessAgent(ctx, ...)` → `runAssessment(ctx, ...)`. Add `ctx context.Context` as the first parameter to each function in the chain that doesn't already have it.

---

## Task 2: Fix sentinel hardcoded line count in assessment prompt

**File:** `internal/sentinel/sentinel.go`, `buildAssessmentPrompt` function (around line 358)

The prompt string hardcodes "last 80 lines" but the actual capture size comes from `w.config.CaptureLines`. If the config value changes, the prompt is misleading.

**Fix:** Add `captureLines int` as a parameter to `buildAssessmentPrompt`:

```go
func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int) string {
	return fmt.Sprintf(`...
Session output (last %d lines):
...`, /* existing args */ captureLines)
}
```

Update the caller in `runAssessment` (or wherever `buildAssessmentPrompt` is called) to pass `w.config.CaptureLines`.

---

## Task 3: Extract shared Register helper for sentinel and consul

**Files:** `internal/sentinel/sentinel.go`, `internal/consul/consul.go`

Both have identical `Register()` patterns that silently swallow `GetAgent` errors:

```go
agent, err := store.GetAgent(id)
if err == nil && agent != nil {
    return nil
}
_, createErr := store.CreateAgent(name, world, role)
return createErr
```

**Fix:** Create a shared helper. The best location depends on the import graph — both sentinel and consul already import `internal/store`. Add the helper to the store package:

**File:** `internal/store/agents.go`

```go
// EnsureAgent creates an agent if it doesn't already exist.
// Returns nil if the agent already exists or was successfully created.
func (s *Store) EnsureAgent(name, world, role string) error {
	id := world + "/" + name
	agent, err := s.GetAgent(id)
	if err == nil && agent != nil {
		return nil // already registered
	}
	if err != nil {
		// GetAgent failed — log context but try CreateAgent anyway.
		// CreateAgent will fail cleanly on unique constraint if agent exists.
		fmt.Fprintf(os.Stderr, "store: GetAgent %q failed, attempting create: %v\n", id, err)
	}
	_, createErr := s.CreateAgent(name, world, role)
	if createErr != nil {
		return fmt.Errorf("failed to ensure agent %q: %w", id, createErr)
	}
	return nil
}
```

Add `"os"` to imports in `agents.go` if not present.

Then update both Register functions:

**Sentinel:**
```go
func (w *Sentinel) Register() error {
	return w.sphereStore.EnsureAgent("sentinel", w.config.World, "sentinel")
}
```

**Consul:**
```go
func (d *Consul) Register() error {
	return d.sphereStore.EnsureAgent("consul", "sphere", "consul")
}
```

---

## Task 4: Remove dead plural function from consul

**File:** `internal/consul/consul.go`

The `plural()` function (around line 254) has no callers and 0% coverage. It's dead code.

**Fix:** Delete the entire function:

```go
// DELETE THIS:
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
```

---

## Task 5: Make GateTimeout a time.Duration in forge Config

**File:** `internal/forge/forge.go`

`GateTimeout` is stored as a `string` and parsed on every `RunGates` call. Invalid values are silently ignored at runtime.

**Fix:** Change the type to `time.Duration`:

```go
type Config struct {
	PollInterval time.Duration
	ClaimTTL     time.Duration
	MaxAttempts  int
	TargetBranch string
	QualityGates []string
	GateTimeout  time.Duration // gate execution timeout (default: 5m)
}

func DefaultConfig() Config {
	return Config{
		PollInterval: 10 * time.Second,
		ClaimTTL:     30 * time.Minute,
		MaxAttempts:  3,
		TargetBranch: "main",
		QualityGates: []string{"go test ./..."},
		GateTimeout:  5 * time.Minute,
	}
}
```

**File:** `internal/forge/toolbox.go`, `RunGates` function

Simplify the timeout logic:

```go
func (r *Forge) RunGates() ([]GateResult, error) {
	timeout := r.cfg.GateTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// ... rest unchanged, use timeout directly
```

**File:** `internal/config/world_config.go`

The world config stores `GateTimeout` as a TOML string (e.g., `gate_timeout = "5m"`). Add validation in the `Validate()` function:

```go
func (c WorldConfig) Validate() error {
	if c.Agents.Capacity < 0 {
		return fmt.Errorf("agents.capacity must be >= 0, got %d", c.Agents.Capacity)
	}
	if c.Agents.ModelTier != "" {
		switch c.Agents.ModelTier {
		case "sonnet", "opus", "haiku":
			// valid
		default:
			return fmt.Errorf("agents.model_tier must be sonnet, opus, or haiku; got %q", c.Agents.ModelTier)
		}
	}
	if c.Forge.GateTimeout != "" {
		if _, err := time.ParseDuration(c.Forge.GateTimeout); err != nil {
			return fmt.Errorf("forge.gate_timeout %q is not a valid duration: %w", c.Forge.GateTimeout, err)
		}
	}
	return nil
}
```

Add `"time"` to imports in `world_config.go` if not already present.

Update wherever `forge.Config` is constructed from `WorldConfig` to parse the duration once:

Search for where `forge.Config{...GateTimeout: ...}` is built (likely in `cmd/forge.go` or `cmd/sentinel.go`). Change the assignment from passing the raw string to parsing it:

```go
var gateTimeout time.Duration
if worldCfg.Forge.GateTimeout != "" {
	gateTimeout, _ = time.ParseDuration(worldCfg.Forge.GateTimeout)
}
if gateTimeout == 0 {
	gateTimeout = 5 * time.Minute
}
cfg := forge.Config{
	// ...
	GateTimeout: gateTimeout,
}
```

---

## Task 6: Remove dead consul backoff tracking

**File:** `internal/prefect/prefect.go`, `startConsul` function (around line 347)

The line `s.backoff[consulSessionName] = s.backoff[consulSessionName] + 1` tracks consul restarts, but `resetBackoffForIdle()` only loops over agent IDs — it never clears the consul backoff. The value grows monotonically and is never read.

**Fix:** Remove the backoff tracking line from `startConsul`:

```go
// DELETE THIS LINE (around line 347):
s.backoff[consulSessionName] = s.backoff[consulSessionName] + 1
```

---

## Task 7: Fix MailNotifier byte-level truncation

**File:** `internal/escalation/mail.go`, around lines 24-26

`desc[:80]` counts bytes, not runes. Truncating at byte 80 can split a multi-byte UTF-8 character.

**Fix:**

```go
// Before:
if len(desc) > 80 {
    desc = desc[:80]
}

// After:
if len([]rune(desc)) > 80 {
    desc = string([]rune(desc)[:80])
}
```

Also fix the hardcoded timestamp format on line 29 to use `time.RFC3339`:

```go
// Before:
esc.CreatedAt.Format("2006-01-02T15:04:05Z")

// After:
esc.CreatedAt.Format(time.RFC3339)
```

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify the consul builds without the `plural` function: `go build ./internal/consul/`
- Verify GateTimeout validation: add a test or manually confirm that `WorldConfig{Forge: ForgeConfig{GateTimeout: "banana"}}.Validate()` returns an error

## Commit

```
fix(sentinel,consul,forge,prefect,escalation): arc 1 review-5 — context propagation, shared register, GateTimeout duration
```
